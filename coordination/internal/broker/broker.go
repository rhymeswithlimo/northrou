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
	ipConns  map[string]int      // client IP -> live websocket count

	// connect is a code-validity oracle (it answers differently for a real vs
	// unknown code), and the connection code is now the sole credential a client
	// authenticates with at the box. Rate-limit connect per client IP and
	// globally so the oracle cannot be used to enumerate valid codes.
	connectPerIP  *limiter
	connectGlobal *limiter
}

const (
	// maxConnsPerIP bounds concurrent signaling sockets from one source IP, so a
	// single client cannot exhaust memory by opening sockets without bound.
	maxConnsPerIP = 40
	// maxServers bounds total registrations, a backstop against registration-
	// flood memory growth.
	maxServers = 100_000
	// maxSessionsPerConn bounds live pairing sessions initiated by one client
	// conn, so repeated connect cannot pile up sessions (and, downstream,
	// PeerConnections on the home server).
	maxSessionsPerConn = 20
)

// session pairs a client and a home server for signaling relay.
type session struct {
	server *conn
	client *conn
}

// conn wraps a signaling WebSocket with a write mutex (concurrent writes are
// not allowed by the ws library).
type conn struct {
	ws       *websocket.Conn
	mu       sync.Mutex
	code     string // set for registered servers
	serverID string // set for registered servers; binds the code to one server
	role     string
	ip       string // client IP (for rate limiting connect)
	sessions int    // live sessions this (client) conn has initiated
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
		ipConns:  map[string]int{},
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
	if !h.admit(c) {
		_ = ws.Close(websocket.StatusPolicyViolation, "too many connections")
		return
	}
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
//
// The connection code is a SYMMETRIC secret (every client of a household knows
// it), so it cannot by itself authenticate a registrant. To stop a code-holder
// from hijacking a live registration - registering their own socket for someone
// else's code to intercept that household's clients - a code is bound to the
// server_id of whoever first registers it, and a registration is refused if a
// DIFFERENT server_id already holds that code live. The real box reconnecting
// (same server_id) is allowed to replace its own stale entry. server_id is never
// shared with clients (only the code is), so an attacker who knows only the code
// cannot match it. Residual risk: an attacker who also learns the server_id, or
// who registers before the real box ever does, can still squat.
func (h *Hub) register(ctx context.Context, c *conn, msg Message) error {
	if msg.Code == "" {
		return errors.New("register requires a code")
	}
	if msg.ServerID == "" {
		return errors.New("register requires a server_id")
	}
	// Match on a canonical form so a box that registers "NR-ABCDE-FGHJK" pairs
	// with a client that connects with "NRABCDEFGHJK" (the web client strips the
	// internal dash before sending). Both sides must agree, so canonicalize here
	// - the single point where register and connect meet.
	code := canonicalCode(msg.Code)
	h.mu.Lock()
	// A conn registers exactly one code; refuse a second (would leak the first's
	// map entry, since cleanup only removes the current code).
	if c.role == "server" {
		h.mu.Unlock()
		return errors.New("already registered")
	}
	if existing, ok := h.servers[code]; ok && existing != c && existing.serverID != msg.ServerID {
		h.mu.Unlock()
		return errors.New("code already registered to another server")
	}
	if len(h.servers) >= maxServers {
		h.mu.Unlock()
		return errors.New("server capacity reached")
	}
	c.role = "server"
	c.code = code
	c.serverID = msg.ServerID
	h.servers[code] = c
	h.mu.Unlock()
	// Never log the raw code: it is the household's master pairing credential and
	// the coordinator sees every server's. Identify the registration by server id.
	slog.Info("home server registered", "server_id", msg.ServerID)
	return c.send(ctx, Message{Type: TypeRegistered})
}

// admit bounds concurrent sockets per source IP. It returns false when the cap
// is already reached; otherwise it records the new connection.
func (h *Hub) admit(c *conn) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.ipConns[c.ip] >= maxConnsPerIP {
		return false
	}
	h.ipConns[c.ip]++
	return true
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
	server, ok := h.servers[canonicalCode(msg.Code)]
	if !ok {
		h.mu.Unlock()
		return errors.New("no server registered for that code")
	}
	if client.sessions >= maxSessionsPerConn {
		h.mu.Unlock()
		return errors.New("too many active sessions")
	}
	client.role = "client"
	client.sessions++
	sid := randomID()
	h.sessions[sid] = &session{server: server, client: client}
	h.mu.Unlock()

	slog.Info("session paired", "session", sid)
	if err := server.send(ctx, Message{Type: TypePaired, Session: sid}); err != nil {
		return err
	}
	return client.send(ctx, Message{Type: TypePaired, Session: sid})
}

// relay forwards a signaling message to the other party in its session. The
// sender MUST be a member of the session it names, so a third conn that guesses
// or learns a session id cannot inject signaling into someone else's pairing.
func (h *Hub) relay(ctx context.Context, from *conn, msg Message) error {
	h.mu.Lock()
	sess, ok := h.sessions[msg.Session]
	h.mu.Unlock()
	if !ok {
		return errors.New("unknown session")
	}
	if from != sess.server && from != sess.client {
		return errors.New("not a member of that session")
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
	if n := h.ipConns[c.ip]; n > 1 {
		h.ipConns[c.ip] = n - 1
	} else {
		delete(h.ipConns, c.ip)
	}
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

// canonicalCode normalizes a connection code for matching: uppercased, with
// every non-alphanumeric character (dashes, spaces) removed. A home server may
// register "NR-ABCDE-FGHJK" while a client connects with "nr abcde fghjk" or a
// dash-stripped "NRABCDEFGHJK"; all must resolve to the same key. This mirrors
// the box's own normalizeConnectionCode, so the credential the client presents
// at the coordinator and later at the box are judged by the same rule.
func canonicalCode(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range strings.ToUpper(s) {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func randomID() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand failed: " + err.Error()) // a broken RNG is fatal
	}
	return hex.EncodeToString(b)
}

// clientIP extracts the client's IP for rate limiting. The official coordinator
// runs behind Cloudflare, which sets CF-Connecting-IP to the real client and,
// crucially, OVERWRITES any client-supplied value - so it cannot be spoofed to
// rotate rate-limit keys. The old code trusted the leftmost X-Forwarded-For
// entry, which a client sets freely; that defeated the per-IP limiter entirely
// (every request could carry a fresh fake IP). Prefer CF-Connecting-IP; fall
// back to the real TCP peer. A self-hoster behind a different proxy that does not
// set CF-Connecting-IP degrades to per-proxy-IP limiting (the global limiter
// still applies), which is safe, not bypassable.
func clientIP(r *http.Request) string {
	if cf := strings.TrimSpace(r.Header.Get("CF-Connecting-IP")); cf != "" {
		return cf
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
