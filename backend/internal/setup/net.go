// Package setup holds helpers for Northrou's first-run experience. The actual
// account/media configuration is performed through the /api/setup endpoints,
// driven by the terminal wizard in internal/tui.
package setup

import "net"

// LocalIPv4s lists this machine's non-loopback IPv4 addresses. On a headless
// box "localhost" refers to the box itself, not whatever device the operator
// is reading from, so setup surfaces these LAN addresses everywhere it prints
// a URL.
func LocalIPv4s() []string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}
	var ips []string
	for _, addr := range addrs {
		ipNet, ok := addr.(*net.IPNet)
		if !ok || ipNet.IP.IsLoopback() {
			continue
		}
		if v4 := ipNet.IP.To4(); v4 != nil {
			ips = append(ips, v4.String())
		}
	}
	return ips
}
