package wifi

import (
	"context"
	"net"
)

// AccessPoint representa uma rede Wi-Fi disponível identificada durante um scan.
type AccessPoint struct {
	SSID      string
	BSSID     string
	Strength  uint8
	Frequency uint32
	Secured   bool
}

// Connection representa o estado e os dados da rede Wi-Fi atualmente conectada.
type Connection struct {
	SSID      string
	Interface string
	IPv4      net.IP
}

// Manager define o contrato para operações de gerenciamento Wi-Fi.
// Qualquer implementação (ex: NetworkManager via D-Bus) deve satisfazer esta interface.
type Manager interface {
	List(ctx context.Context) ([]AccessPoint, error)
	Connect(ctx context.Context, ssid string, password string) error
	Disconnect(ctx context.Context) error
	Current(ctx context.Context) (Connection, error)
}