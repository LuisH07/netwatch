package network

import (
	"errors"
	"net"
	"testing"

	"github.com/vishvananda/netlink"
)

func withRouteStubs(t *testing.T, routeGet func(net.IP) ([]netlink.Route, error), linkByIndex func(int) (netlink.Link, error)) {
	t.Helper()
	origRouteGet, origLinkByIndex := netlinkRouteGet, netlinkLinkByIndex
	netlinkRouteGet = routeGet
	netlinkLinkByIndex = linkByIndex
	t.Cleanup(func() {
		netlinkRouteGet = origRouteGet
		netlinkLinkByIndex = origLinkByIndex
	})
}

func TestGetDefaultRoute_Success(t *testing.T) {
	gw := net.ParseIP("192.168.1.1")
	withRouteStubs(t,
		func(net.IP) ([]netlink.Route, error) {
			return []netlink.Route{{LinkIndex: 3, Gw: gw}}, nil
		},
		func(idx int) (netlink.Link, error) {
			if idx != 3 {
				t.Fatalf("LinkByIndex called with unexpected index %d", idx)
			}
			return &netlink.Device{LinkAttrs: netlink.LinkAttrs{Name: "eth0"}}, nil
		},
	)

	route, err := GetDefaultRoute()
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if !route.Gateway.Equal(gw) {
		t.Errorf("Gateway = %v, want %v", route.Gateway, gw)
	}
	if route.Interface != "eth0" {
		t.Errorf("Interface = %q, want %q", route.Interface, "eth0")
	}
}

func TestGetDefaultRoute_NetlinkError(t *testing.T) {
	withRouteStubs(t,
		func(net.IP) ([]netlink.Route, error) { return nil, errors.New("netlink unavailable") },
		func(int) (netlink.Link, error) { t.Fatal("LinkByIndex should not be called"); return nil, nil },
	)

	_, err := GetDefaultRoute()
	if err == nil {
		t.Fatal("expected error when netlink.RouteGet fails, got nil")
	}
}

func TestGetDefaultRoute_NoRoutes(t *testing.T) {
	withRouteStubs(t,
		func(net.IP) ([]netlink.Route, error) { return nil, nil },
		func(int) (netlink.Link, error) { t.Fatal("LinkByIndex should not be called"); return nil, nil },
	)

	_, err := GetDefaultRoute()
	if err == nil {
		t.Fatal("expected error when no routes are returned, got nil")
	}
}

func TestGetDefaultRoute_NoGateway(t *testing.T) {
	withRouteStubs(t,
		func(net.IP) ([]netlink.Route, error) {
			return []netlink.Route{{LinkIndex: 3, Gw: nil}}, nil
		},
		func(int) (netlink.Link, error) { t.Fatal("LinkByIndex should not be called"); return nil, nil },
	)

	_, err := GetDefaultRoute()
	if !errors.Is(err, ErrNoDefaultRoute) {
		t.Fatalf("expected ErrNoDefaultRoute, got: %v", err)
	}
}

func TestGetDefaultRoute_LinkByIndexError(t *testing.T) {
	withRouteStubs(t,
		func(net.IP) ([]netlink.Route, error) {
			return []netlink.Route{{LinkIndex: 3, Gw: net.ParseIP("10.0.0.1")}}, nil
		},
		func(int) (netlink.Link, error) { return nil, errors.New("no such link") },
	)

	_, err := GetDefaultRoute()
	if err == nil {
		t.Fatal("expected error when LinkByIndex fails, got nil")
	}
}
