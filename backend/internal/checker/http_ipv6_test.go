package checker

import (
	"net"
	"testing"
)

// TestIsConnRefused dials a real closed loopback port: the chain is
// *net.OpError → *os.SyscallError → syscall.Errno (a value, not a pointer),
// so classification must match the whole chain, not a *syscall.Errno.
func TestIsConnRefused(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	_, err = net.Dial("tcp", addr)
	if err == nil {
		t.Fatalf("dial %s succeeded, want connection refused", addr)
	}
	if !isConnRefused(err) {
		t.Errorf("isConnRefused(%v) = false, want true", err)
	}
	if isConnRefused(nil) {
		t.Error("isConnRefused(nil) = true, want false")
	}
}
