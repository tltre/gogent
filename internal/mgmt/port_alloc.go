package mgmt

import (
	"fmt"
	"net"
)

// IsPortAvailable checks whether a TCP port is available to bind to on all interfaces.
// Uses ":port" (wildcard address) to match how agent processes bind,
// preventing false positives on platforms where 127.0.0.1:port and 0.0.0.0:port
// may be treated as separate endpoints.
func IsPortAvailable(port int) bool {
	addr := fmt.Sprintf(":%d", port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return false
	}
	defer ln.Close()
	return true
}

// AllocatePort finds an available TCP port starting from basePort+1.
// It probes up to 100 consecutive ports and returns the first available one.
func AllocatePort(basePort int) (int, error) {
	for i := 1; i <= 100; i++ {
		port := basePort + i
		if IsPortAvailable(port) {
			return port, nil
		}
	}
	return 0, fmt.Errorf("no available port after 100 attempts starting from %d", basePort)
}
