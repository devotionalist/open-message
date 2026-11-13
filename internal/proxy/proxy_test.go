package proxy

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestStopDrainsListener(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	p := New(Config{ListenAddr: addr, DefaultBroker: "127.0.0.1:1", ShutdownGrace: 200 * time.Millisecond}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := p.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err := p.Stop(); err != nil {
		t.Fatal(err)
	}
	c, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
	if err == nil {
		_ = c.Close()
		t.Fatal("expected dial fail after Stop")
	}
}
