package network

import (
	"errors"
	"fmt"
	"net"
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

// GetDefaultInterface obtém a interface de rede principal ativa do sistema.
// Ele itera sobre as interfaces físicas (ignorando loopback) em busca da primeira com um IPv4 válido.
func GetDefaultInterface() (InterfaceInfo, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return InterfaceInfo{}, fmt.Errorf("falha ao listar interfaces: %w", err)
	}

	for _, iface := range interfaces {
		// Ignora interfaces que estão caídas ou que são de loopback (ex: lo)
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue // Pula se não conseguir ler os endereços desta interface
		}

		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}

			if ip == nil || ip.IsLoopback() {
				continue
			}

			ip4 := ip.To4()
			if ip4 == nil {
				continue // Ignora IPv6 conforme escopo do projeto
			}

			return InterfaceInfo{
				Name: iface.Name,
				Up:   true,
				MAC:  iface.HardwareAddr.String(),
				IPv4: ip4,
			}, nil
		}
	}

	return InterfaceInfo{}, ErrNoActiveInterface
}