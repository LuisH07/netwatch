package network

import (
	"context"
	"testing"
	"time"
)

func TestResolve_ValidHost(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	res, err := Resolve(ctx, "localhost")
	if err != nil {
		t.Fatalf("expected localhost to resolve, got error: %v", err)
	}
	if len(res.Addresses) == 0 {
		t.Error("expected at least one resolved address")
	}
	if res.Latency < 0 {
		t.Errorf("Latency = %v, want >= 0", res.Latency)
	}
}

func TestResolve_InvalidHost(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err := Resolve(ctx, "this-domain-should-not-exist.invalid")
	if err == nil {
		t.Fatal("expected an error resolving a nonexistent domain, got nil")
	}
}

func TestResolve_CanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := Resolve(ctx, "localhost")
	if err == nil {
		t.Fatal("expected an error when context is already canceled, got nil")
	}
}
