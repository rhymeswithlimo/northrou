// Package remote provides Northrou's peer-to-peer remote access: a WebRTC peer
// that registers with the coordination server, hole-punches a direct connection
// to remote clients, and tunnels the ordinary HTTP API over a WebRTC data
// channel. Media therefore flows directly between client and home server; the
// coordination server only brokers the handshake and never sees media bytes.
//
// The tunnel multiplexes HTTP by opening one data channel per request. Each
// channel carries length-prefixed frames: a JSON request envelope, then a JSON
// response envelope, then body chunks terminated by a zero-length frame.
package remote

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
)

type ctxKey int

const tunnelKey ctxKey = iota

// WithTunnel marks a context as belonging to a request that arrived over the
// WebRTC tunnel (a remote client), as opposed to a direct local/LAN connection.
func WithTunnel(ctx context.Context) context.Context {
	return context.WithValue(ctx, tunnelKey, true)
}

// IsTunnel reports whether r arrived over the tunnel. Direct requests (a
// same-origin browser on the LAN, or the CLI) return false. The flag is stamped
// by ServeConn and cannot be set by the remote client.
func IsTunnel(r *http.Request) bool {
	v, _ := r.Context().Value(tunnelKey).(bool)
	return v
}

// IsLocal reports whether r should be treated as a trusted local request — the
// basis of the admin gate and code-free pairing. A request is local only when it
// (1) did NOT arrive over the tunnel, and (2) has a peer address that is loopback
// or in a private/link-local range. The second condition matters: "not tunneled"
// alone is not "trusted", because the box's HTTP port can be bound to all
// interfaces (the default) or published by Docker, so a request straight off the
// public internet is also non-tunnel. Requiring a private/loopback peer means
// exposing the port does not hand out admin to the world. The peer is taken from
// r.RemoteAddr (the real TCP peer), never a client-supplied X-Forwarded-For.
func IsLocal(r *http.Request) bool {
	if IsTunnel(r) {
		return false
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast()
}

// reqEnvelope is the first frame a client sends on a request data channel.
type reqEnvelope struct {
	Method string      `json:"method"`
	URL    string      `json:"url"`
	Header http.Header `json:"header"`
	Body   []byte      `json:"body,omitempty"`
}

// respEnvelope is the first frame the server sends back; body chunks follow.
type respEnvelope struct {
	Status int         `json:"status"`
	Header http.Header `json:"header"`
}

const maxFrame = 16 << 20 // 16 MiB cap per frame

// writeFrame writes a length-prefixed frame.
func writeFrame(w io.Writer, payload []byte) error {
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], uint32(len(payload)))
	if _, err := w.Write(hdr[:]); err != nil {
		return err
	}
	if len(payload) == 0 {
		return nil
	}
	_, err := w.Write(payload)
	return err
}

// readFrame reads a length-prefixed frame. A zero-length frame returns (nil,nil).
func readFrame(r io.Reader) ([]byte, error) {
	var hdr [4]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return nil, err
	}
	n := binary.BigEndian.Uint32(hdr[:])
	if n == 0 {
		return nil, nil
	}
	if n > maxFrame {
		return nil, fmt.Errorf("frame too large: %d", n)
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

// ServeConn reads one HTTP request from rwc, dispatches it to handler, and
// writes the response back over rwc. It is used server-side for each inbound
// request data channel.
func ServeConn(rwc io.ReadWriteCloser, handler http.Handler) error {
	defer rwc.Close()

	frame, err := readFrame(rwc)
	if err != nil {
		return err
	}
	var env reqEnvelope
	if err := json.Unmarshal(frame, &env); err != nil {
		return err
	}

	req := httptest.NewRequest(env.Method, env.URL, bytes.NewReader(env.Body))
	req.RemoteAddr = "webrtc:0"
	req = req.WithContext(WithTunnel(req.Context()))
	for k, vs := range env.Header {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}

	rw := &dcResponseWriter{w: rwc, header: http.Header{}, status: http.StatusOK}
	handler.ServeHTTP(rw, req)
	if err := rw.finish(); err != nil {
		return err
	}
	return writeFrame(rwc, nil) // EOF
}

// dcResponseWriter streams an http response over a data channel: it sends the
// response envelope on the first Write (or on finish), then body chunks.
type dcResponseWriter struct {
	w      io.Writer
	header http.Header
	status int
	wrote  bool
}

func (d *dcResponseWriter) Header() http.Header { return d.header }

func (d *dcResponseWriter) WriteHeader(code int) {
	if !d.wrote {
		d.status = code
	}
}

func (d *dcResponseWriter) Write(p []byte) (int, error) {
	if err := d.ensureEnvelope(); err != nil {
		return 0, err
	}
	if err := writeFrame(d.w, p); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (d *dcResponseWriter) ensureEnvelope() error {
	if d.wrote {
		return nil
	}
	d.wrote = true
	env, _ := json.Marshal(respEnvelope{Status: d.status, Header: d.header})
	return writeFrame(d.w, env)
}

// finish flushes the envelope for empty responses.
func (d *dcResponseWriter) finish() error { return d.ensureEnvelope() }

// RoundTrip performs one HTTP request over rwc and returns the response. The
// response Body streams body frames until EOF; closing it closes rwc.
func RoundTrip(rwc io.ReadWriteCloser, req *http.Request) (*http.Response, error) {
	var body []byte
	if req.Body != nil {
		b, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		body = b
	}
	env := reqEnvelope{Method: req.Method, URL: req.URL.RequestURI(), Header: req.Header, Body: body}
	data, _ := json.Marshal(env)
	if err := writeFrame(rwc, data); err != nil {
		return nil, err
	}

	respFrame, err := readFrame(rwc)
	if err != nil {
		return nil, err
	}
	var re respEnvelope
	if err := json.Unmarshal(respFrame, &re); err != nil {
		return nil, err
	}
	return &http.Response{
		StatusCode: re.Status,
		Header:     re.Header,
		Body:       &frameReader{rwc: rwc},
	}, nil
}

// frameReader exposes the body frames of a tunneled response as an io.Reader,
// stopping at the zero-length EOF frame.
type frameReader struct {
	rwc io.ReadWriteCloser
	buf []byte
	eof bool
}

func (f *frameReader) Read(p []byte) (int, error) {
	for len(f.buf) == 0 {
		if f.eof {
			return 0, io.EOF
		}
		frame, err := readFrame(f.rwc)
		if err != nil {
			return 0, err
		}
		if frame == nil { // zero-length frame => EOF
			f.eof = true
			return 0, io.EOF
		}
		f.buf = frame
	}
	n := copy(p, f.buf)
	f.buf = f.buf[n:]
	return n, nil
}

func (f *frameReader) Close() error { return f.rwc.Close() }
