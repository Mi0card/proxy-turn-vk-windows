//go:build windows

package main

import (
	"fmt"
	"log"
	"net"
	"time"
)

// listenUDP binds a UDP socket. On Windows a just-exited client process can
// briefly hold the port (lingering socket), so the bind is retried a few times
// and falls back to a random dynamic port, mirroring the pre-upstream engine.
func listenUDP(addr string) (net.PacketConn, error) {
	conn, err := net.ListenPacket("udp", addr)
	if err == nil {
		return conn, nil
	}
	for i := 0; i < 5; i++ {
		log.Printf("[ОЖИДАНИЕ] Порт %s занят (возможно, старый процесс завершается). Жду... (%d/5)", addr, i+1)
		time.Sleep(time.Second)
		conn, err = net.ListenPacket("udp", addr)
		if err == nil {
			return conn, nil
		}
	}
	log.Printf("[АВТО-ПОРТ] Порт %s всё ещё занят. Пробую случайный динамический порт...", addr)
	conn, err = net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("bind dynamic port: %w", err)
	}
	return conn, nil
}
