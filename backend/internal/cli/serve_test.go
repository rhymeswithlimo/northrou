package cli

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"testing"
)

func TestPortConflict(t *testing.T) {
	// Bind an ephemeral port, then provoke a real EADDRINUSE against it.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_, bindErr := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if bindErr == nil {
		t.Skip("expected an in-use bind to fail")
	}
	// Free the port so portConflict's alreadyServing() probe fails fast
	// (connection refused) rather than waiting out its timeout. The captured
	// bindErr still carries EADDRINUSE.
	ln.Close()

	if got := portConflict(nil, port); got != nil {
		t.Errorf("nil error should pass through, got %v", got)
	}

	sentinel := errors.New("some other failure")
	if got := portConflict(sentinel, port); got != sentinel {
		t.Errorf("non-conflict error should pass through unchanged, got %v", got)
	}

	// A real EADDRINUSE with nothing answering health checks: another program
	// holds the port, so the user gets fix-it guidance.
	got := portConflict(fmt.Errorf("listen: %w", bindErr), port)
	if got == nil || !strings.Contains(got.Error(), "in use by another program") {
		t.Errorf("expected foreign-port guidance, got %v", got)
	}
	if !strings.Contains(got.Error(), "[server]") {
		t.Errorf("guidance should point at the [server] config section, got %q", got)
	}
}

func TestFirstFreePort(t *testing.T) {
	p := firstFreePort(20000)
	if p == 0 {
		t.Skip("no free port found in the scan range")
	}
	ln, err := net.Listen("tcp", net.JoinHostPort("", strconv.Itoa(p)))
	if err != nil {
		t.Fatalf("suggested port %d was not bindable: %v", p, err)
	}
	ln.Close()
}
