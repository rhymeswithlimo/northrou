package broker

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

func dial(t *testing.T, url string) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { c.Close(websocket.StatusNormalClosure, "") })
	return c
}

func write(t *testing.T, c *websocket.Conn, m Message) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := wsjson.Write(ctx, c, m); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func read(t *testing.T, c *websocket.Conn) Message {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var m Message
	if err := wsjson.Read(ctx, c, &m); err != nil {
		t.Fatalf("read: %v", err)
	}
	return m
}

func newBrokerServer(t *testing.T) string {
	t.Helper()
	hub := NewHub()
	srv := httptest.NewServer(http.HandlerFunc(hub.ServeWS))
	t.Cleanup(srv.Close)
	return "ws" + strings.TrimPrefix(srv.URL, "http")
}

func TestPairingAndRelay(t *testing.T) {
	url := newBrokerServer(t)

	// Home server registers.
	server := dial(t, url)
	write(t, server, Message{Type: TypeRegister, Role: "server", Code: "NR-TEST"})
	if m := read(t, server); m.Type != TypeRegistered {
		t.Fatalf("expected registered, got %+v", m)
	}

	// Client connects with the code.
	client := dial(t, url)
	write(t, client, Message{Type: TypeConnect, Role: "client", Code: "NR-TEST"})

	// Both sides get a paired message with the same session id.
	serverPaired := read(t, server)
	clientPaired := read(t, client)
	if serverPaired.Type != TypePaired || clientPaired.Type != TypePaired {
		t.Fatalf("expected paired on both sides")
	}
	if serverPaired.Session == "" || serverPaired.Session != clientPaired.Session {
		t.Fatalf("session ids differ: %q vs %q", serverPaired.Session, clientPaired.Session)
	}
	sid := serverPaired.Session

	// Client sends an offer; the server should receive it relayed.
	write(t, client, Message{Type: TypeOffer, Session: sid, SDP: "OFFER_SDP"})
	if m := read(t, server); m.Type != TypeOffer || m.SDP != "OFFER_SDP" {
		t.Fatalf("server did not receive relayed offer: %+v", m)
	}

	// Server answers; the client should receive it relayed.
	write(t, server, Message{Type: TypeAnswer, Session: sid, SDP: "ANSWER_SDP"})
	if m := read(t, client); m.Type != TypeAnswer || m.SDP != "ANSWER_SDP" {
		t.Fatalf("client did not receive relayed answer: %+v", m)
	}
}

func TestConnectUnknownCode(t *testing.T) {
	url := newBrokerServer(t)
	client := dial(t, url)
	write(t, client, Message{Type: TypeConnect, Role: "client", Code: "NOPE"})
	if m := read(t, client); m.Type != TypeError {
		t.Fatalf("expected error for unknown code, got %+v", m)
	}
}
