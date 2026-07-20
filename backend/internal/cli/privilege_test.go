package cli

import (
	"errors"
	"io/fs"
	"os"
	"strings"
	"testing"
)

func TestElevationHint(t *testing.T) {
	// A realistic permission error: the exact shape selfupdate.Apply returns
	// when it cannot create the replacement binary as a non-root user.
	permErr := &os.PathError{Op: "open", Path: "/usr/local/bin/.northrou.new", Err: fs.ErrPermission}
	otherErr := errors.New("network is unreachable")

	tests := []struct {
		name        string
		err         error
		euid        int
		goos        string
		wantSame    bool   // hint should return err unchanged
		wantSnippet string // substring the wrapped message must contain
		mustWrap    bool   // errors.Is(result, err) must still hold
	}{
		{name: "nil error", err: nil, euid: 1000, goos: "linux", wantSame: true},
		{name: "non-permission error", err: otherErr, euid: 1000, goos: "linux", wantSame: true},
		{name: "permission but already root", err: permErr, euid: 0, goos: "linux", wantSame: true},
		{
			name: "permission non-root linux", err: permErr, euid: 1000, goos: "linux",
			wantSnippet: "sudo northrou update", mustWrap: true,
		},
		{
			name: "permission non-root darwin", err: permErr, euid: 501, goos: "darwin",
			wantSnippet: "sudo northrou update", mustWrap: true,
		},
		{
			// os.Geteuid() is -1 on Windows, so the euid==0 guard never trips.
			name: "permission windows", err: permErr, euid: -1, goos: "windows",
			wantSnippet: "Administrator", mustWrap: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := elevationHint(tt.err, "northrou update", tt.euid, tt.goos)
			if tt.wantSame {
				if got != tt.err {
					t.Fatalf("expected err returned unchanged, got %v", got)
				}
				return
			}
			if !strings.Contains(got.Error(), tt.wantSnippet) {
				t.Errorf("message %q missing %q", got.Error(), tt.wantSnippet)
			}
			if tt.mustWrap && !errors.Is(got, tt.err) {
				t.Errorf("wrapped error lost its cause; errors.Is failed")
			}
		})
	}
}
