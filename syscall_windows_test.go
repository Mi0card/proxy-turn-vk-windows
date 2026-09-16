//go:build windows

package main

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// lookupProcessByPort по локальному порту собственного соединения обязан
// вернуть имя текущего процесса (тестового бинарника).
func TestLookupProcessByPortSelf(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()

	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	port := conn.LocalAddr().(*net.TCPAddr).Port
	got := lookupProcessByPort(port)
	if got == "" {
		t.Fatalf("lookupProcessByPort(%d) = пусто, ожидалось имя процесса", port)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Base(exe)
	if !strings.EqualFold(got, want) {
		t.Fatalf("lookupProcessByPort(%d) = %q, ожидалось %q", port, got, want)
	}
}

func TestLookupProcessByPortInvalid(t *testing.T) {
	for _, port := range []int{0, -1, 70000} {
		if got := lookupProcessByPort(port); got != "" {
			t.Errorf("lookupProcessByPort(%d) = %q, ожидалось пусто", port, got)
		}
	}
}
