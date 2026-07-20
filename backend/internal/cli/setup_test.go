package cli

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

func TestAlreadyServing(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	_, portStr, err := net.SplitHostPort(srv.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatal(err)
	}
	if !alreadyServing(port) {
		t.Error("expected alreadyServing to detect the running test server")
	}

	// A port nothing is listening on should report false, not hang or panic.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	freePort := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	if alreadyServing(freePort) {
		t.Error("expected alreadyServing to report false for a closed port")
	}
}
