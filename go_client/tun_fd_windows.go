//go:build windows

package main

import (
	"fmt"
	"os"
)

// recvTunFD receives a TUN fd over a unix socket (Android-only feature,
// -mode rawtun). Windows has no unix sockets and runs userspace netstack
// instead, so this mode is unavailable here.
func recvTunFD(sockPath string) (*os.File, error) {
	return nil, fmt.Errorf("tun-fd-sock: not supported on windows")
}