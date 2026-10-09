package main

import (
	"testing"

	"github.com/Nel-sokchhunly/fragile/notes"
)

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

func TestRunInvalidSubagentProvider(t *testing.T) {
	err := run(notes.Config{Provider: "claude", Addr: "127.0.0.1:7777"}, "unknown_provider", "")
	if err == nil {
		t.Fatal("expected error for invalid subagent provider")
	}
}

