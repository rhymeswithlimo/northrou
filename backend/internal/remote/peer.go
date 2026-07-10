package remote

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/pion/webrtc/v4"
)

// signalMessage mirrors the coordination broker's envelope.
type signalMessage struct {
	Type      string `json:"type"`
	Role      string `json:"role,omitempty"`
	ServerID  string `json:"server_id,omitempty"`
	Code      string `json:"code,omitempty"`
	Session   string `json:"session,omitempty"`
	SDP       string `json:"sdp,omitempty"`
	Candidate string `json:"candidate,omitempty"`
	Error     string `json:"error,omitempty"`
}

// Peer is the home server's WebRTC endpoint. It registers with the coordination
// server and, for each remote client session, establishes a direct connection
// and serves the HTTP API over a data-channel tunnel.
type Peer struct {
	coordURL string
	serverID string
	code     string
	handler  http.Handler
	api      *webrtc.API
	ice      []webrtc.ICEServer

	mu       sync.Mutex
	ws       *websocket.Conn
	sessions map[string]*webrtc.PeerConnection
}

// NewPeer builds a Peer that tunnels to handler. coordURL is the ws:// or wss://
// signaling endpoint (e.g. wss://coord.northrou.app/ws).
func NewPeer(coordURL, serverID, code string, handler http.Handler) *Peer {
	se := webrtc.SettingEngine{}
	se.DetachDataChannels() // use io.ReadWriteCloser data channels
	return &Peer{
		coordURL: coordURL,
		serverID: serverID,
		code:     code,
		handler:  handler,
		api:      webrtc.NewAPI(webrtc.WithSettingEngine(se)),
		ice: []webrtc.ICEServer{
			{URLs: []string{"stun:stun.l.google.com:19302"}},
		},
		sessions: map[string]*webrtc.PeerConnection{},
	}
}

// Run connects to the coordinator and services signaling until ctx is done,
// reconnecting with backoff on failure.
func (p *Peer) Run(ctx context.Context) {
	backoff := time.Second
	for ctx.Err() == nil {
		if err := p.connectOnce(ctx); err != nil {
			slog.Warn("remote signaling disconnected", "err", err, "retry_in", backoff)
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			if backoff < 30*time.Second {
				backoff *= 2
			}
			continue
		}
		backoff = time.Second
	}
}

// connectOnce runs a single signaling session lifetime.
func (p *Peer) connectOnce(ctx context.Context) error {
	ws, _, err := websocket.Dial(ctx, p.coordURL, nil)
	if err != nil {
		return err
	}
	p.mu.Lock()
	p.ws = ws
	p.mu.Unlock()
	defer ws.Close(websocket.StatusNormalClosure, "bye")

	if err := p.send(ctx, signalMessage{Type: "register", Role: "server", ServerID: p.serverID, Code: p.code}); err != nil {
		return err
	}
	slog.Info("registered with coordination server", "url", p.coordURL, "code", p.code)

	for {
		var msg signalMessage
		if err := wsjson.Read(ctx, ws, &msg); err != nil {
			return err
		}
		if err := p.handle(ctx, msg); err != nil {
			slog.Debug("signaling handling error", "type", msg.Type, "err", err)
		}
	}
}

func (p *Peer) send(ctx context.Context, m signalMessage) error {
	p.mu.Lock()
	ws := p.ws
	p.mu.Unlock()
	if ws == nil {
		return context.Canceled
	}
	return wsjson.Write(ctx, ws, m)
}

func (p *Peer) handle(ctx context.Context, msg signalMessage) error {
	switch msg.Type {
	case "registered":
		return nil
	case "paired":
		return p.newSession(ctx, msg.Session)
	case "offer":
		return p.onOffer(ctx, msg)
	case "candidate":
		return p.onCandidate(msg)
	case "error":
		slog.Warn("coordinator error", "err", msg.Error)
		return nil
	default:
		return nil
	}
}

// newSession creates a PeerConnection for an incoming client session.
func (p *Peer) newSession(ctx context.Context, session string) error {
	pc, err := p.api.NewPeerConnection(webrtc.Configuration{ICEServers: p.ice})
	if err != nil {
		return err
	}
	p.mu.Lock()
	p.sessions[session] = pc
	p.mu.Unlock()

	pc.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c == nil {
			return
		}
		cand, _ := json.Marshal(c.ToJSON())
		_ = p.send(ctx, signalMessage{Type: "candidate", Session: session, Candidate: string(cand)})
	})
	pc.OnConnectionStateChange(func(s webrtc.PeerConnectionState) {
		slog.Debug("peer connection state", "session", session, "state", s.String())
		if s == webrtc.PeerConnectionStateFailed || s == webrtc.PeerConnectionStateClosed {
			p.closeSession(session)
		}
	})
	// Each inbound data channel carries one HTTP request; serve it via the API.
	pc.OnDataChannel(func(dc *webrtc.DataChannel) {
		dc.OnOpen(func() {
			raw, err := dc.Detach()
			if err != nil {
				slog.Debug("data channel detach failed", "err", err)
				return
			}
			go func() {
				if err := ServeConn(raw, p.handler); err != nil {
					slog.Debug("tunnel serve error", "err", err)
				}
			}()
		})
	})
	return nil
}

func (p *Peer) onOffer(ctx context.Context, msg signalMessage) error {
	p.mu.Lock()
	pc := p.sessions[msg.Session]
	p.mu.Unlock()
	if pc == nil {
		return nil
	}
	if err := pc.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeOffer, SDP: msg.SDP,
	}); err != nil {
		return err
	}
	answer, err := pc.CreateAnswer(nil)
	if err != nil {
		return err
	}
	if err := pc.SetLocalDescription(answer); err != nil {
		return err
	}
	return p.send(ctx, signalMessage{Type: "answer", Session: msg.Session, SDP: answer.SDP})
}

func (p *Peer) onCandidate(msg signalMessage) error {
	p.mu.Lock()
	pc := p.sessions[msg.Session]
	p.mu.Unlock()
	if pc == nil {
		return nil
	}
	var init webrtc.ICECandidateInit
	if err := json.Unmarshal([]byte(msg.Candidate), &init); err != nil {
		return err
	}
	return pc.AddICECandidate(init)
}

func (p *Peer) closeSession(session string) {
	p.mu.Lock()
	pc := p.sessions[session]
	delete(p.sessions, session)
	p.mu.Unlock()
	if pc != nil {
		_ = pc.Close()
	}
}
