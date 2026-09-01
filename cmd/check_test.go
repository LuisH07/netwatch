package cmd

import (
	"context"
	"errors"
	"net"
	"netwatch/internal/network"
	"testing"
	"time"
)

// withCheckStubs substitui os pontos de injeção de checkCmd por implementações fake e
// restaura os originais ao final do teste.
func withCheckStubs(t *testing.T,
	iface func() (network.InterfaceInfo, error),
	route func() (network.Route, error),
	ping func(context.Context, string) (network.PingResult, error),
	resolve func(context.Context, string) (network.DNSResult, error),
	tcp func(context.Context, string) (time.Duration, error),
) {
	t.Helper()
	origIface, origRoute, origPing, origResolve, origTCP := getDefaultInterface, getDefaultRoute, pingFn, resolveFn, checkTCPFn
	getDefaultInterface, getDefaultRoute, pingFn, resolveFn, checkTCPFn = iface, route, ping, resolve, tcp
	t.Cleanup(func() {
		getDefaultInterface, getDefaultRoute, pingFn, resolveFn, checkTCPFn = origIface, origRoute, origPing, origResolve, origTCP
	})
}

func okInterface() (network.InterfaceInfo, error) {
	return network.InterfaceInfo{Name: "eth0", Up: true, IPv4: net.ParseIP("10.0.0.5")}, nil
}

func okRoute() (network.Route, error) {
	return network.Route{Gateway: net.ParseIP("10.0.0.1"), Interface: "eth0"}, nil
}

func okPing(context.Context, string) (network.PingResult, error) {
	return network.PingResult{Latency: time.Millisecond, TTL: 64}, nil
}

func okResolve(context.Context, string) (network.DNSResult, error) {
	return network.DNSResult{Addresses: []net.IP{net.ParseIP("1.2.3.4")}, Latency: time.Millisecond}, nil
}

func okTCP(context.Context, string) (time.Duration, error) {
	return time.Millisecond, nil
}

func runCheckCmd(t *testing.T) error {
	t.Helper()
	return checkCmd.RunE(checkCmd, nil)
}

func TestCheckCmd_AllHealthy(t *testing.T) {
	withCheckStubs(t, okInterface, okRoute, okPing, okResolve, okTCP)

	if err := runCheckCmd(t); err != nil {
		t.Fatalf("expected nil error (HEALTHY), got: %v", err)
	}
}

// TestCheckCmd_InterfaceDown é uma regressão: checkCmd costumava sempre imprimir "UP" e
// nunca contabilizava o estado real da interface no diagnóstico, mesmo quando
// GetDefaultInterface reportava Up=false (ex.: rota obsoleta apontando para uma interface caída).
func TestCheckCmd_InterfaceDown(t *testing.T) {
	withCheckStubs(t,
		func() (network.InterfaceInfo, error) {
			return network.InterfaceInfo{Name: "eth0", Up: false, IPv4: net.ParseIP("10.0.0.5")}, nil
		},
		okRoute, okPing, okResolve, okTCP,
	)

	err := runCheckCmd(t)
	var exitErr *exitError
	if !errors.As(err, &exitErr) || exitErr.code != 1 {
		t.Fatalf("expected exitError{code:1} (DEGRADED) when the interface is down, got: %v", err)
	}
}

func TestCheckCmd_InterfaceFailure(t *testing.T) {
	withCheckStubs(t,
		func() (network.InterfaceInfo, error) { return network.InterfaceInfo{}, errors.New("no interface") },
		okRoute, okPing, okResolve, okTCP,
	)

	err := runCheckCmd(t)
	var exitErr *exitError
	if !errors.As(err, &exitErr) || exitErr.code != 2 {
		t.Fatalf("expected exitError{code:2}, got: %v", err)
	}
}

func TestCheckCmd_RouteFailure(t *testing.T) {
	withCheckStubs(t,
		okInterface,
		func() (network.Route, error) { return network.Route{}, errors.New("no route") },
		okPing, okResolve, okTCP,
	)

	err := runCheckCmd(t)
	var exitErr *exitError
	if !errors.As(err, &exitErr) || exitErr.code != 2 {
		t.Fatalf("expected exitError{code:2}, got: %v", err)
	}
}

func TestCheckCmd_ICMPFailure_Degraded(t *testing.T) {
	withCheckStubs(t, okInterface, okRoute,
		func(context.Context, string) (network.PingResult, error) {
			return network.PingResult{}, errors.New("ping failed")
		},
		okResolve, okTCP,
	)

	err := runCheckCmd(t)
	var exitErr *exitError
	if !errors.As(err, &exitErr) || exitErr.code != 1 {
		t.Fatalf("expected exitError{code:1} (DEGRADED), got: %v", err)
	}
}

func TestCheckCmd_DNSFailure_Degraded(t *testing.T) {
	withCheckStubs(t, okInterface, okRoute, okPing,
		func(context.Context, string) (network.DNSResult, error) {
			return network.DNSResult{}, errors.New("dns failed")
		},
		okTCP,
	)

	err := runCheckCmd(t)
	var exitErr *exitError
	if !errors.As(err, &exitErr) || exitErr.code != 1 {
		t.Fatalf("expected exitError{code:1} (DEGRADED), got: %v", err)
	}
}

func TestCheckCmd_TCPFailure_Degraded(t *testing.T) {
	withCheckStubs(t, okInterface, okRoute, okPing, okResolve,
		func(context.Context, string) (time.Duration, error) {
			return 0, errors.New("tcp failed")
		},
	)

	err := runCheckCmd(t)
	var exitErr *exitError
	if !errors.As(err, &exitErr) || exitErr.code != 1 {
		t.Fatalf("expected exitError{code:1} (DEGRADED), got: %v", err)
	}
}
