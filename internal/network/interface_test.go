package network

import (
	"errors"
	"net"
	"testing"

	"github.com/vishvananda/netlink"
)

func withInterfaceStubs(t *testing.T,
	routeGet func(net.IP) ([]netlink.Route, error),
	interfaceByIndex func(int) (*net.Interface, error),
	addrs func(*net.Interface) ([]net.Addr, error),
) {
	t.Helper()
	origRouteGet, origInterfaceByIndex, origAddrs := netlinkRouteGet, netInterfaceByIndex, ifaceAddrs
	netlinkRouteGet = routeGet
	netInterfaceByIndex = interfaceByIndex
	ifaceAddrs = addrs
	t.Cleanup(func() {
		netlinkRouteGet = origRouteGet
		netInterfaceByIndex = origInterfaceByIndex
		ifaceAddrs = origAddrs
	})
}

func TestGetDefaultInterface_Success(t *testing.T) {
	mac, _ := net.ParseMAC("aa:bb:cc:dd:ee:ff")
	withInterfaceStubs(t,
		func(net.IP) ([]netlink.Route, error) {
			return []netlink.Route{{LinkIndex: 7}}, nil
		},
		func(idx int) (*net.Interface, error) {
			if idx != 7 {
				t.Fatalf("InterfaceByIndex called with unexpected index %d", idx)
			}
			return &net.Interface{Index: 7, Name: "wlan0", Flags: net.FlagUp, HardwareAddr: mac}, nil
		},
		func(*net.Interface) ([]net.Addr, error) {
			_, ipnet, _ := net.ParseCIDR("10.0.0.5/24")
			ipnet.IP = net.ParseIP("10.0.0.5")
			return []net.Addr{ipnet}, nil
		},
	)

	info, err := GetDefaultInterface()
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if info.Name != "wlan0" {
		t.Errorf("Name = %q, want %q", info.Name, "wlan0")
	}
	if !info.Up {
		t.Error("expected Up = true")
	}
	if info.MAC != mac.String() {
		t.Errorf("MAC = %q, want %q", info.MAC, mac.String())
	}
	if info.IPv4.String() != "10.0.0.5" {
		t.Errorf("IPv4 = %v, want 10.0.0.5", info.IPv4)
	}
}

func TestGetDefaultInterface_SkipsLoopbackAndIPv6(t *testing.T) {
	withInterfaceStubs(t,
		func(net.IP) ([]netlink.Route, error) {
			return []netlink.Route{{LinkIndex: 2}}, nil
		},
		func(int) (*net.Interface, error) {
			return &net.Interface{Index: 2, Name: "eth0", Flags: net.FlagUp}, nil
		},
		func(*net.Interface) ([]net.Addr, error) {
			loopback := &net.IPNet{IP: net.ParseIP("127.0.0.1"), Mask: net.CIDRMask(8, 32)}
			ipv6 := &net.IPNet{IP: net.ParseIP("fe80::1"), Mask: net.CIDRMask(64, 128)}
			valid := &net.IPNet{IP: net.ParseIP("192.168.5.10"), Mask: net.CIDRMask(24, 32)}
			return []net.Addr{loopback, ipv6, valid}, nil
		},
	)

	info, err := GetDefaultInterface()
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if info.IPv4.String() != "192.168.5.10" {
		t.Errorf("IPv4 = %v, want 192.168.5.10 (loopback/IPv6 addresses should be skipped)", info.IPv4)
	}
}

func TestGetDefaultInterface_NoIPv4(t *testing.T) {
	withInterfaceStubs(t,
		func(net.IP) ([]netlink.Route, error) {
			return []netlink.Route{{LinkIndex: 2}}, nil
		},
		func(int) (*net.Interface, error) {
			return &net.Interface{Index: 2, Name: "eth0", Flags: net.FlagUp}, nil
		},
		func(*net.Interface) ([]net.Addr, error) {
			ipv6 := &net.IPNet{IP: net.ParseIP("fe80::1"), Mask: net.CIDRMask(64, 128)}
			return []net.Addr{ipv6}, nil
		},
	)

	_, err := GetDefaultInterface()
	if err == nil {
		t.Fatal("expected error when interface has no IPv4 address, got nil")
	}
}

func TestGetDefaultInterface_NoRoutes(t *testing.T) {
	withInterfaceStubs(t,
		func(net.IP) ([]netlink.Route, error) { return nil, nil },
		func(int) (*net.Interface, error) { t.Fatal("InterfaceByIndex should not be called"); return nil, nil },
		func(*net.Interface) ([]net.Addr, error) { t.Fatal("ifaceAddrs should not be called"); return nil, nil },
	)

	_, err := GetDefaultInterface()
	if err == nil {
		t.Fatal("expected error when no routes are returned, got nil")
	}
}

func TestGetDefaultInterface_InvalidLinkIndex(t *testing.T) {
	withInterfaceStubs(t,
		func(net.IP) ([]netlink.Route, error) { return []netlink.Route{{LinkIndex: 0}}, nil },
		func(int) (*net.Interface, error) { t.Fatal("InterfaceByIndex should not be called"); return nil, nil },
		func(*net.Interface) ([]net.Addr, error) { t.Fatal("ifaceAddrs should not be called"); return nil, nil },
	)

	_, err := GetDefaultInterface()
	if err == nil {
		t.Fatal("expected error for invalid link index, got nil")
	}
}

func TestGetDefaultInterface_InterfaceByIndexError(t *testing.T) {
	withInterfaceStubs(t,
		func(net.IP) ([]netlink.Route, error) { return []netlink.Route{{LinkIndex: 9}}, nil },
		func(int) (*net.Interface, error) { return nil, errors.New("no such interface") },
		func(*net.Interface) ([]net.Addr, error) { t.Fatal("ifaceAddrs should not be called"); return nil, nil },
	)

	_, err := GetDefaultInterface()
	if err == nil {
		t.Fatal("expected error when InterfaceByIndex fails, got nil")
	}
}

func TestGetDefaultInterface_AddrsError(t *testing.T) {
	withInterfaceStubs(t,
		func(net.IP) ([]netlink.Route, error) { return []netlink.Route{{LinkIndex: 9}}, nil },
		func(int) (*net.Interface, error) {
			return &net.Interface{Index: 9, Name: "eth0", Flags: net.FlagUp}, nil
		},
		func(*net.Interface) ([]net.Addr, error) { return nil, errors.New("failed to read addrs") },
	)

	_, err := GetDefaultInterface()
	if err == nil {
		t.Fatal("expected error when reading addresses fails, got nil")
	}
}
