package remote

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/pion/webrtc/v4"
)

// testHandler is a small API used by the tunnel tests.
func testHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/ping", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("pong"))
	})
	mux.HandleFunc("/echo", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write(b)
	})
	// /tunnel reports whether the handler sees the request as tunneled. This is
	// the basis of the admin gate: admin actions are refused for tunneled
	// (remote) requests, allowed for direct (local) ones.
	mux.HandleFunc("/tunnel", func(w http.ResponseWriter, r *http.Request) {
		if IsTunnel(r) {
			_, _ = w.Write([]byte("tunnel"))
		} else {
			_, _ = w.Write([]byte("local"))
		}
	})
	// default: 404
	return mux
}

// TestIsLocal covers the trust classification behind the admin gate: only a
// non-tunnel request from a loopback or private/link-local peer is local.
func TestIsLocal(t *testing.T) {
	cases := []struct {
		name       string
		remoteAddr string
		tunnel     bool
		want       bool
	}{
		{"loopback v4", "127.0.0.1:5000", false, true},
		{"loopback v6", "[::1]:5000", false, true},
		{"rfc1918 10", "10.1.2.3:5000", false, true},
		{"rfc1918 172", "172.16.5.9:5000", false, true},
		{"rfc1918 192.168", "192.168.1.42:5000", false, true},
		{"link-local v4", "169.254.10.20:5000", false, true},
		{"ula v6", "[fd00::1]:5000", false, true},
		{"public v4", "203.0.113.5:5000", false, false},
		{"public v6", "[2606:4700::1]:5000", false, false},
		{"private but tunneled", "192.168.1.42:5000", true, false},
		{"unparseable", "webrtc:0", false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req, _ := http.NewRequest("GET", "/", nil)
			req.RemoteAddr = tc.remoteAddr
			if tc.tunnel {
				req = req.WithContext(WithTunnel(req.Context()))
			}
			if got := IsLocal(req); got != tc.want {
				t.Errorf("IsLocal(%q, tunnel=%v) = %v, want %v", tc.remoteAddr, tc.tunnel, got, tc.want)
			}
		})
	}
}

// TestServeConnMarksTunnel proves ServeConn stamps every request it serves as
// tunneled, and that a request built directly (as a same-origin/LAN request is)
// is not. The stamp is set in-process by ServeConn and is not derived from any
// client-supplied header, so a remote client cannot forge "local".
func TestServeConnMarksTunnel(t *testing.T) {
	// Served through the tunnel: IsTunnel must be true.
	req, _ := http.NewRequest("GET", "/tunnel", nil)
	resp := roundTripOverPipe(t, req)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(body) != "tunnel" {
		t.Errorf("tunneled request: IsTunnel reported %q, want tunnel", body)
	}

	// A plain request (not through ServeConn) must be seen as local.
	direct, _ := http.NewRequest("GET", "/tunnel", nil)
	rec := httptest.NewRecorder()
	testHandler().ServeHTTP(rec, direct)
	if got := rec.Body.String(); got != "local" {
		t.Errorf("direct request: IsTunnel reported %q, want local", got)
	}
}

// roundTripOverPipe runs one request through ServeConn/RoundTrip over net.Pipe.
func roundTripOverPipe(t *testing.T, req *http.Request) *http.Response {
	t.Helper()
	clientEnd, serverEnd := net.Pipe()
	go func() { _ = ServeConn(serverEnd, testHandler()) }()
	resp, err := RoundTrip(clientEnd, req)
	if err != nil {
		t.Fatalf("round trip: %v", err)
	}
	return resp
}

func TestTunnelRoundTrip_GET(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "http://x/ping", nil)
	resp := roundTripOverPipe(t, req)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "pong" {
		t.Errorf("body = %q, want pong", body)
	}
}

func TestTunnelRoundTrip_POSTEcho(t *testing.T) {
	req, _ := http.NewRequest(http.MethodPost, "http://x/echo", strings.NewReader("hello tunnel"))
	resp := roundTripOverPipe(t, req)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "hello tunnel" {
		t.Errorf("echo body = %q", body)
	}
}

func TestTunnelRoundTrip_404(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "http://x/missing", nil)
	resp := roundTripOverPipe(t, req)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

// TestTunnelOverRealWebRTC establishes an actual WebRTC data channel between two
// in-process pion peers and tunnels an HTTP request over it, the same path used
// in production, minus the external coordinator.
func TestTunnelOverRealWebRTC(t *testing.T) {
	se := webrtc.SettingEngine{}
	se.DetachDataChannels()
	api := webrtc.NewAPI(webrtc.WithSettingEngine(se))

	server, err := api.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	client, err := api.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	// Server side: serve the API on each inbound data channel.
	server.OnDataChannel(func(dc *webrtc.DataChannel) {
		dc.OnOpen(func() {
			raw, err := dc.Detach()
			if err != nil {
				return
			}
			go ServeConn(raw, testHandler())
		})
	})

	// Client opens a request channel and round-trips once it's open.
	done := make(chan string, 1)
	errc := make(chan error, 1)
	dc, err := client.CreateDataChannel("req", nil)
	if err != nil {
		t.Fatal(err)
	}
	dc.OnOpen(func() {
		raw, err := dc.Detach()
		if err != nil {
			errc <- err
			return
		}
		req, _ := http.NewRequest(http.MethodGet, "http://x/ping", nil)
		resp, err := RoundTrip(raw, req)
		if err != nil {
			errc <- err
			return
		}
		body, _ := io.ReadAll(resp.Body)
		done <- string(body)
	})

	// Non-trickle signaling: exchange full SDP after ICE gathering completes.
	offer, err := client.CreateOffer(nil)
	if err != nil {
		t.Fatal(err)
	}
	gatherClient := webrtc.GatheringCompletePromise(client)
	if err := client.SetLocalDescription(offer); err != nil {
		t.Fatal(err)
	}
	<-gatherClient
	if err := server.SetRemoteDescription(*client.LocalDescription()); err != nil {
		t.Fatal(err)
	}
	answer, err := server.CreateAnswer(nil)
	if err != nil {
		t.Fatal(err)
	}
	gatherServer := webrtc.GatheringCompletePromise(server)
	if err := server.SetLocalDescription(answer); err != nil {
		t.Fatal(err)
	}
	<-gatherServer
	if err := client.SetRemoteDescription(*server.LocalDescription()); err != nil {
		t.Fatal(err)
	}

	select {
	case body := <-done:
		if body != "pong" {
			t.Errorf("tunneled response = %q, want pong", body)
		}
	case err := <-errc:
		t.Fatalf("tunnel error: %v", err)
	case <-time.After(15 * time.Second):
		t.Fatal("timed out establishing WebRTC tunnel")
	}
	_ = context.Background()
}
