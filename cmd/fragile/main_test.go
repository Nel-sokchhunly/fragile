package main

import "testing"

func TestCheckLoopback(t *testing.T) {
	for addr, ok := range map[string]bool{
		"127.0.0.1:7777": true, "localhost:7777": true, "[::1]:7777": true, "127.0.0.2:1": true,
		":7777": false, "0.0.0.0:7777": false, "[::]:7777": false, "192.168.1.5:7777": false,
		"example.com:7777": false, "7777": false, "": false,
	} {
		if err := checkLoopback(addr); (err == nil) != ok {
			t.Errorf("checkLoopback(%q) = %v, want ok=%v", addr, err, ok)
		}
	}
}
