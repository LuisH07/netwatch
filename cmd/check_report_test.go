package cmd

import (
	"context"
	"errors"
	"net"
	"netwatch/internal/network"
	"strings"
	"testing"
	"time"
)

func TestRunDiagnostics_AllHealthy(t *testing.T) {
	withCheckStubs(t, okInterface, okRoute, okPing, okResolve, okTCP)

	r := runDiagnostics()
	if r.InterfaceErr != nil || r.RouteErr != nil {
		t.Fatalf("expected no stage errors, got InterfaceErr=%v RouteErr=%v", r.InterfaceErr, r.RouteErr)
	}
	if r.Degraded {
		t.Error("expected Degraded = false")
	}
	if r.ICMP.Err != nil || r.DNS.Err != nil || r.TCP.Err != nil {
		t.Errorf("expected all connectivity stages to succeed, got ICMP=%v DNS=%v TCP=%v", r.ICMP.Err, r.DNS.Err, r.TCP.Err)
	}
}

func TestRunDiagnostics_InterfaceErrorStopsEarly(t *testing.T) {
	withCheckStubs(t,
		func() (network.InterfaceInfo, error) { return network.InterfaceInfo{}, errors.New("no interface") },
		okRoute, okPing, okResolve, okTCP,
	)

	r := runDiagnostics()
	if r.InterfaceErr == nil {
		t.Fatal("expected InterfaceErr to be set")
	}
	if r.RouteErr != nil {
		t.Error("expected RouteErr to remain nil when interface lookup already failed")
	}
	if r.Route.Gateway != nil || r.Route.Interface != "" {
		t.Error("expected Route to remain zero-value when interface lookup already failed")
	}
}

func TestRunDiagnostics_RouteErrorStopsEarly(t *testing.T) {
	withCheckStubs(t, okInterface,
		func() (network.Route, error) { return network.Route{}, errors.New("no route") },
		okPing, okResolve, okTCP,
	)

	r := runDiagnostics()
	if r.InterfaceErr != nil {
		t.Errorf("expected InterfaceErr = nil, got %v", r.InterfaceErr)
	}
	if r.RouteErr == nil {
		t.Fatal("expected RouteErr to be set")
	}
	if r.ICMP.Err != nil || r.DNS.Err != nil || r.TCP.Err != nil {
		t.Error("expected connectivity stages to never run when route lookup failed")
	}
}

// TestRunDiagnostics_InterfaceDownIsDegradedNotError é uma regressão: uma interface reportada
// como down deve contar como Degraded (problema de rede), não como InterfaceErr (erro de
// execução) — o relatório continua sendo calculado normalmente a partir daí.
func TestRunDiagnostics_InterfaceDownIsDegradedNotError(t *testing.T) {
	withCheckStubs(t,
		func() (network.InterfaceInfo, error) {
			return network.InterfaceInfo{Name: "eth0", Up: false, IPv4: net.ParseIP("10.0.0.5")}, nil
		},
		okRoute, okPing, okResolve, okTCP,
	)

	r := runDiagnostics()
	if r.InterfaceErr != nil {
		t.Errorf("expected InterfaceErr = nil for a down-but-present interface, got %v", r.InterfaceErr)
	}
	if !r.Degraded {
		t.Error("expected Degraded = true when the interface is down")
	}
	if r.RouteErr != nil {
		t.Error("expected the report to keep computing routing/connectivity despite the down interface")
	}
}

func TestRunDiagnostics_ConnectivityFailureMarksDegraded(t *testing.T) {
	withCheckStubs(t, okInterface, okRoute,
		func(context.Context, string) (network.PingResult, error) {
			return network.PingResult{}, errors.New("ping failed")
		},
		okResolve, okTCP,
	)

	r := runDiagnostics()
	if !r.Degraded {
		t.Error("expected Degraded = true when ICMP fails")
	}
	if r.ICMP.Err == nil {
		t.Error("expected ICMP.Err to be set")
	}
}

func TestRenderCheckReport_InterfaceErrorShape(t *testing.T) {
	r := checkReport{InterfaceErr: errors.New("boom")}
	out := renderCheckReport(r)

	if !strings.Contains(out, "NETWATCH CHECK") || !strings.Contains(out, "Interface") {
		t.Errorf("expected header + Interface section, got: %q", out)
	}
	if strings.Contains(out, "Routing") || strings.Contains(out, "Connectivity") {
		t.Errorf("expected the report to stop after the Interface section, got: %q", out)
	}
}

func TestRenderCheckReport_RouteErrorShape(t *testing.T) {
	r := checkReport{
		Interface: network.InterfaceInfo{Name: "eth0", Up: true, IPv4: net.ParseIP("10.0.0.5")},
		RouteErr:  errors.New("boom"),
	}
	out := renderCheckReport(r)

	if !strings.Contains(out, "Interface") || !strings.Contains(out, "eth0") {
		t.Errorf("expected the completed Interface section, got: %q", out)
	}
	if !strings.Contains(out, "Routing") {
		t.Errorf("expected the Routing section header, got: %q", out)
	}
	if strings.Contains(out, "Connectivity") {
		t.Errorf("expected the report to stop before Connectivity, got: %q", out)
	}
}

func TestRenderCheckReport_FullHealthyShape(t *testing.T) {
	r := checkReport{
		Interface: network.InterfaceInfo{Name: "eth0", Up: true, IPv4: net.ParseIP("10.0.0.5"), MAC: "aa:bb:cc:dd:ee:ff"},
		Route:     network.Route{Gateway: net.ParseIP("10.0.0.1"), Interface: "eth0"},
		ICMPLabel: "ICMP (10.0.0.1)", ICMP: checkStage{Latency: 2 * time.Millisecond, TTL: 64},
		DNSLabel: "DNS (google.com)", DNS: checkStage{Latency: time.Millisecond, Addresses: []net.IP{net.ParseIP("1.2.3.4")}},
		TCPLabel: "TCP (9.9.9.9:443)", TCP: checkStage{Latency: 5 * time.Millisecond},
		Degraded: false,
	}
	out := renderCheckReport(r)

	for _, want := range []string{"Interface", "Routing", "Connectivity", "eth0", "10.0.0.1", "TTL 64", "1.2.3.4", "HEALTHY"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected output to contain %q, got: %q", want, out)
		}
	}
	if strings.Contains(out, "DEGRADED") {
		t.Errorf("expected HEALTHY output to not mention DEGRADED, got: %q", out)
	}
}

func TestRenderCheckReport_DegradedShape(t *testing.T) {
	r := checkReport{
		Interface: network.InterfaceInfo{Name: "eth0", Up: true, IPv4: net.ParseIP("10.0.0.5")},
		Route:     network.Route{Gateway: net.ParseIP("10.0.0.1"), Interface: "eth0"},
		ICMPLabel: "ICMP (10.0.0.1)", ICMP: checkStage{Err: errors.New("timeout")},
		DNSLabel: "DNS (google.com)", DNS: checkStage{Latency: time.Millisecond},
		TCPLabel: "TCP (9.9.9.9:443)", TCP: checkStage{Latency: time.Millisecond},
		Degraded: true,
	}
	out := renderCheckReport(r)

	if !strings.Contains(out, "DEGRADED") {
		t.Errorf("expected DEGRADED in output, got: %q", out)
	}
	if !strings.Contains(out, "Erro: timeout") {
		t.Errorf("expected the ICMP error to be rendered, got: %q", out)
	}
}
