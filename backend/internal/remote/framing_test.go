package remote

import (
	"encoding/binary"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/pion/webrtc/v4"
)

// A data channel is SCTP: message-oriented. ServeConn reads it as a byte stream
// via io.ReadFull(hdr[:4]), and pion returns io.ErrShortBuffer when the read
// buffer is smaller than the message. So a client that packs a frame's header
// and payload into ONE message breaks the server, while one that writes them
// as two (exactly as writeFrame does) works.
//
// This is not theoretical: the JS client in js/api/tunnel.js sent single-message
// frames and every request died with the channel closing. These tests pin the
// contract from the server's side so the next client port cannot repeat it.

func TestServeConnRejectsSingleMessageFrame(t *testing.T) {
	server, client, cleanup := realPeers(t)
	defer cleanup()

	served := make(chan error, 1)
	server.OnDataChannel(func(dc *webrtc.DataChannel) {
		dc.OnOpen(func() {
			raw, err := dc.Detach()
			if err != nil {
				served <- err
				return
			}
			served <- ServeConn(raw, testHandler())
		})
	})

	dc, err := client.CreateDataChannel("req", nil)
	if err != nil {
		t.Fatal(err)
	}
	dc.OnOpen(func() {
		raw, err := dc.Detach()
		if err != nil {
			return
		}
		env, _ := json.Marshal(reqEnvelope{Method: "GET", URL: "/ping", Header: http.Header{}})

		// The wrong way: header and payload as a single message.
		buf := make([]byte, 4+len(env))
		binary.BigEndian.PutUint32(buf[:4], uint32(len(env)))
		copy(buf[4:], env)
		_, _ = raw.Write(buf)
	})

	handshake(t, client, server)

	select {
	case err := <-served:
		if err == nil {
			t.Fatal("ServeConn accepted a single-message frame; the JS client depends on this failing loudly rather than hanging")
		}
	case <-time.After(15 * time.Second):
		t.Fatal("timed out")
	}
}

func TestServeConnAcceptsSplitFrame(t *testing.T) {
	server, client, cleanup := realPeers(t)
	defer cleanup()

	server.OnDataChannel(func(dc *webrtc.DataChannel) {
		dc.OnOpen(func() {
			raw, err := dc.Detach()
			if err != nil {
				return
			}
			go ServeConn(raw, testHandler())
		})
	})

	body := make(chan string, 1)
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
		env, _ := json.Marshal(reqEnvelope{Method: "GET", URL: "/ping", Header: http.Header{}})

		// The right way, and what js/api/tunnel.js does: header, then payload.
		var hdr [4]byte
		binary.BigEndian.PutUint32(hdr[:], uint32(len(env)))
		if _, err := raw.Write(hdr[:]); err != nil {
			errc <- err
			return
		}
		if _, err := raw.Write(env); err != nil {
			errc <- err
			return
		}

		resp, err := readResponse(raw)
		if err != nil {
			errc <- err
			return
		}
		body <- resp
	})

	handshake(t, client, server)

	select {
	case got := <-body:
		if got != "pong" {
			t.Errorf("tunneled body = %q, want pong", got)
		}
	case err := <-errc:
		t.Fatalf("split-frame round trip failed: %v", err)
	case <-time.After(15 * time.Second):
		t.Fatal("timed out")
	}
}

// readResponse reads the response envelope and body frames the way a client must.
func readResponse(r io.Reader) (string, error) {
	head, err := readFrame(r)
	if err != nil {
		return "", err
	}
	var re respEnvelope
	if err := json.Unmarshal(head, &re); err != nil {
		return "", err
	}
	var out []byte
	for {
		chunk, err := readFrame(r)
		if err != nil {
			return "", err
		}
		if chunk == nil { // zero-length frame: EOF
			return string(out), nil
		}
		out = append(out, chunk...)
	}
}

func realPeers(t *testing.T) (server, client *webrtc.PeerConnection, cleanup func()) {
	t.Helper()
	se := webrtc.SettingEngine{}
	se.DetachDataChannels()
	api := webrtc.NewAPI(webrtc.WithSettingEngine(se))

	server, err := api.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	client, err = api.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		server.Close()
		t.Fatal(err)
	}
	return server, client, func() { client.Close(); server.Close() }
}

// handshake does non-trickle signalling: full SDP once ICE has gathered.
func handshake(t *testing.T, client, server *webrtc.PeerConnection) {
	t.Helper()
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
}
