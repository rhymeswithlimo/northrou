package remote

import (
	"context"
	"io"
	"net"
	"net/http"
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
	// default: 404
	return mux
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
