package service

import (
	"strings"
	"testing"
)

func TestLidSwitchWarning(t *testing.T) {
	tests := []struct {
		name     string
		goos     string
		conf     string
		wantWarn bool
	}{
		{"non-linux never warns", "darwin", "HandleLidSwitch=suspend", false},
		{"unset defaults to suspend and warns", "linux", "", true},
		{"explicit ignore is silent", "linux", "HandleLidSwitch=ignore\n", false},
		{"explicit suspend warns", "linux", "HandleLidSwitch=suspend\n", true},
		{"commented out falls back to default and warns", "linux", "#HandleLidSwitch=ignore\n", true},
		{"external power key alone warns", "linux", "HandleLidSwitchExternalPower=suspend\n", true},
		{"external power explicitly ignored is silent regardless of lid switch default", "linux", "HandleLidSwitchExternalPower=ignore\n", false},
		{"both set to ignore is silent", "linux", "HandleLidSwitch=ignore\nHandleLidSwitchExternalPower=ignore\n", false},
		{"whitespace and case tolerated", "linux", "  HANDLELIDSWITCH = Ignore  \n", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := lidSwitchWarning(tt.goos, strings.NewReader(tt.conf))
			if (got != "") != tt.wantWarn {
				t.Errorf("lidSwitchWarning(%q, %q) = %q, want non-empty=%v", tt.goos, tt.conf, got, tt.wantWarn)
			}
		})
	}
}
