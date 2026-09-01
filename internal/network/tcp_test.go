package network

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestCheckTCP_Success(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start local listener: %v", err)
	}
	defer ln.Close()

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			conn.Close()
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	latency, err := CheckTCP(ctx, ln.Addr().String())
	if err != nil {
		t.Fatalf("expected successful TCP connection, got error: %v", err)
	}
	if latency < 0 {
		t.Errorf("latency = %v, want >= 0", latency)
	}
}

func TestCheckTCP_ConnectionRefused(t *testing.T) {
	// Abre e fecha um listener para obter uma porta local livre em que ninguém mais escuta.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to allocate a free port: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err = CheckTCP(ctx, addr)
	if err == nil {
		t.Fatal("expected connection refused error, got nil")
	}
}

func TestCheckTCP_ContextTimeout(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := CheckTCP(ctx, "127.0.0.1:1")
	if err == nil {
		t.Fatal("expected an error when context is already canceled, got nil")
	}
}
