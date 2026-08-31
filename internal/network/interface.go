package network

import (
	"errors"
	"fmt"
	"net"

	"github.com/vishvananda/netlink"
)

// ErrNoActiveInterface indica que nenhuma interface apta foi encontrada no sistema.
var ErrNoActiveInterface = errors.New("nenhuma interface de rede ativa com IPv4 encontrada")

// InterfaceInfo contém os dados essenciais de uma interface de rede.
type InterfaceInfo struct {
	Name string
	Up   bool
	MAC  string
	IPv4 net.IP
}

// GetDefaultInterface obtém a interface de rede principal ativa do sistema
// consultando diretamente a rota padrão do kernel via Netlink.
func GetDefaultInterface() (InterfaceInfo, error) {
	// Consulta a rota padrão do kernel para o IP de destino genérico (ex: 1.1.1.1)
	routes, err := netlink.RouteGet(net.ParseIP("1.1.1.1"))
	if err != nil || len(routes) == 0 {
		return InterfaceInfo{}, fmt.Errorf("falha ao determinar rota padrão via netlink: %w", err)
	}

	// A primeira rota retornada é a escolhida pelo kernel para o tráfego padrão
	route := routes[0]
	if route.LinkIndex <= 0 {
		return InterfaceInfo{}, errors.New("índice de link inválido na rota padrão")
	}

	// Obtém a interface de rede física/virtual associada àquele índice de link
	iface, err := net.InterfaceByIndex(route.LinkIndex)
	if err != nil {
		return InterfaceInfo{}, fmt.Errorf("falha ao buscar interface por índice %d: %w", route.LinkIndex, err)
	}

	// Valida se a interface está UP
	isUp := iface.Flags&net.FlagUp != 0

	// Coleta os endereços IP associados à interface correta
	addrs, err := iface.Addrs()
	if err != nil {
		return InterfaceInfo{}, fmt.Errorf("falha ao ler endereços da interface %s: %w", iface.Name, err)
	}

	var ipv4 net.IP
	for _, addr := range addrs {
		var ip net.IP
		switch v := addr.(type) {
		case *net.IPNet:
			ip = v.IP
		case *net.IPAddr:
			ip = v.IP
		}

		if ip != nil && !ip.IsLoopback() {
			if ip4 := ip.To4(); ip4 != nil {
				ipv4 = ip4
				break
			}
		}
	}

	if ipv4 == nil {
		return InterfaceInfo{}, fmt.Errorf("interface de rota padrão %s não possui endereço IPv4 válido", iface.Name)
	}

	// Trata segurança para interfaces virtuais sem MAC (ex: tun/tap ou wg sem hardware addr)
	macStr := ""
	if iface.HardwareAddr != nil {
		macStr = iface.HardwareAddr.String()
	}

	return InterfaceInfo{
		Name: iface.Name,
		Up:   isUp,
		MAC:  macStr,
		IPv4: ipv4,
	}, nil
}