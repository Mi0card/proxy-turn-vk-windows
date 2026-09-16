//go:build darwin

package main

import "testing"

func TestLocalPortOf(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"127.0.0.1:54321->1.2.3.4:443", 54321},
		{"127.0.0.1:54321", 54321},
		{"[::1]:8080->[::1]:443", 8080},
		{"", 0},
		{"noport", 0},
	}
	for _, c := range cases {
		if got := localPortOf(c.in); got != c.want {
			t.Errorf("localPortOf(%q) = %d, ожидалось %d", c.in, got, c.want)
		}
	}
}
