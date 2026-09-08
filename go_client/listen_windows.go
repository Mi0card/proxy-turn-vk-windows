//go:build windows

package main

import (
	"net"
)

// listenUDP binds a UDP socket. SO_REUSEADDR is unnecessary on Windows: UDP
// ports are reclaimed immediately on process exit, so a quick restart can
// rebind 127.0.0.1:port without trouble.
func listenUDP(addr string) (net.PacketConn, error) {
	return net.ListenPacket("udp", addr)
}