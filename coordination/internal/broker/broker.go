package broker

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

// Hub tracks registered home servers (by connection code) and active signaling
// sessions. All state is in-memory; the broker is disposable and horizontally
// restartable.
type Hub struct {
	mu       sync.Mutex
	servers  map[string]*conn    // code -> registered home server
	sessions map[string]*session // session id -> pair

	// connect is a code-validity oracle (it answers differently for a real vs
	// unknown code), and the connection code is now the sole credential a client
	// authenticates with at the box. Rate-limit connect per client IP and
	// globally so the oracle cannot be used to enumerate valid codes.
	connectPerIP  *limiter
	connectGlobal *limiter
}

// session pairs a client and a home server for signaling relay.
type session struct {
	server *conn
	client *conn
}

// conn wraps a signaling WebSocket with a write mutex (concurrent writes are
// not allowed by the ws library).
type conn struct {
	ws   *websocket.Conn
	mu   sync.Mutex
	code string // set for registered servers
	role string
	ip   string // client IP (for rate limiting connect)
}

func (c *conn) send(ctx context.Context, m Message) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return wsjson.Write(ctx, c.ws, m)
}

// NewHub creates an empty Hub.
func NewHub() *Hub {
	return &Hub{
		servers:  map[string]*conn{},
		sessions: map[string]*session{},
		// Legitimate clients connect a handful of times; these caps are far above
		// that but far below what would make enumerating a ~50-bit code space
		// feasible.
		connectPerIP:  newLimiter(time.Minute, 30),
		connectGlobal: newLimiter(time.Minute, 600),
	}
}

// ServeWS is the http.Handler for the /ws signaling endpoint.
func (h *Hub) ServeWS(w http.ResponseWriter, r *http.Request) {
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: []string{"*"}, // clients connect from anywhere
	})
	if err != nil {
		return
	}
	c := &conn{ws: ws, ip: clientIP(r)}
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	defer h.cleanup(c)

	// Home servers hold a long-lived registration socket that is silent between
	// pairings. An idle-timeout proxy in front of the coordinator (Cloudflare
	// closes idle WebSockets after ~100s) would drop it and silently unregister
	// the box, so ping to keep it flowing. Ping is safe alongside the read loop.
	go keepAlive(ctx, ws)

	for {
		var msg Message
		if err := wsjson.Read(ctx, ws, &msg); err != nil {
			return
		}
		if err := h.handle(ctx, c, msg); err != nil {
			_ = c.send(ctx, Message{Type: TypeError, Error: err.Error()})
		}
	}
}

// keepAlive pings ws on an interval short enough to beat idle-timeout proxies,
// closing the connection (which unblocks the read loop) if a ping goes
// unanswered so a half-dead socket doesn't linger as a stale registration.
func keepAlive(ctx context.Context, ws *websocket.Conn) {
	t := time.NewTicker(pingInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			pctx, cancel := context.WithTimeout(ctx, pingTimeout)
			err := ws.Ping(pctx)
			cancel()
			if err != nil {
				_ = ws.Close(websocket.StatusGoingAway, "keepalive failed")
				return
			}
		}
	}
}

const (
	// pingInterval is well under the ~100s idle window of proxies like Cloudflare.
	pingInterval = 30 * time.Second
	pingTimeout  = 10 * time.Second
)

// handle dispatches one incoming message.
func (h *Hub) handle(ctx context.Context, c *conn, msg Message) error {
	switch msg.Type {
	case TypeRegister:
		return h.register(ctx, c, msg)
	case TypeConnect:
		return h.connect(ctx, c, msg)
	case TypeOffer, TypeAnswer, TypeCandidate:
		return h.relay(ctx, c, msg)
	default:
		return errors.New("unknown message type: " + msg.Type)
	}
}

// register records a home server keyed by its connection code.
func (h *Hub) register(ctx context.Context, c *conn, msg Message) error {
	if msg.Code == "" {
		return errors.New("register requires a code")
	}
	h.mu.Lock()
	c.role = "server"
	c.code = msg.Code
	h.servers[msg.Code] = c
	h.mu.Unlock()
	slog.Info("home server registered", "code", msg.Code, "server_id", msg.ServerID)
	return c.send(ctx, Message{Type: TypeRegistered})
}

// connect pairs a client with a registered server and notifies both.
func (h *Hub) connect(ctx context.Context, client *conn, msg Message) error {
	// Throttle before the lookup: connect reveals whether a code is registered,
	// and the code is the client's credential, so an unbounded oracle would let
	// an attacker enumerate valid codes.
	if !h.connectPerIP.allow(client.ip) || !h.connectGlobal.allow("*") {
		return errors.New("too many attempts; try again shortly")
	}
	h.mu.Lock()
	server, ok := h.servers[msg.Code]
	if !ok {
		h.mu.Unlock()
		return errors.New("no server registered for that code")
	}
	client.role = "client"
	sid := randomID()
	h.sessions[sid] = &session{server: server, client: client}
	h.mu.Unlock()

	slog.Info("session paired", "session", sid, "code", msg.Code)
	if err := server.send(ctx, Message{Type: TypePaired, Session: sid}); err != nil {
		return err
	}
	return client.send(ctx, Message{Type: TypePaired, Session: sid})
}

// relay forwards a signaling message to the other party in its session.
func (h *Hub) relay(ctx context.Context, from *conn, msg Message) error {
	h.mu.Lock()
	sess, ok := h.sessions[msg.Session]
	h.mu.Unlock()
	if !ok {
		return errors.New("unknown session")
	}
	dst := sess.server
	if from == sess.server {
		dst = sess.client
	}
	return dst.send(ctx, msg)
}

// cleanup removes a disconnected conn from the registry and tears down any
// sessions it participated in.
func (h *Hub) cleanup(c *conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if c.code != "" && h.servers[c.code] == c {
		delete(h.servers, c.code)
	}
	for id, s := range h.sessions {
		if s.server == c || s.client == c {
			delete(h.sessions, id)
		}
	}
}

// Stats returns current registry sizes (for the health/metrics endpoint).
func (h *Hub) Stats() (servers, sessions int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.servers), len(h.sessions)
}

func randomID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// clientIP extracts the client's IP for rate limiting. The coordinator runs
// behind a reverse proxy (Caddy) and Cloudflare, so the real client is in
// X-Forwarded-For (first hop) rather than RemoteAddr, which would be the proxy.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if i := strings.IndexByte(xff, ','); i >= 0 {
			return strings.TrimSpace(xff[:i])
		}
		return strings.TrimSpace(xff)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
