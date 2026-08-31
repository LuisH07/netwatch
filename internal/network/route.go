package network

import (
	"errors"
	"fmt"
	"net"

	"github.com/vishvananda/netlink"
)

// ErrNoDefaultRoute indica que nenhuma rota padrão IPv4 foi encontrada no sistema.
var ErrNoDefaultRoute = errors.New("nenhuma rota padrão IPv4 encontrada")

// Route representa as informações essenciais da rota de gateway padrão.
type Route struct {
	Gateway   net.IP
	Interface string
}

// GetDefaultRoute consulta a tabela de roteamento do kernel via Netlink 
// em busca da rota padrão (destino 0.0.0.0/0) e retorna o IP do gateway e a interface associada.
func GetDefaultRoute() (Route, error) {
	// Lista apenas rotas IPv4
	routes, err := netlink.RouteList(nil, netlink.FAMILY_V4)
	if err != nil {
		return Route{}, fmt.Errorf("falha ao listar rotas via netlink: %w", err)
	}

	for _, route := range routes {
		// A rota padrão no netlink tem o Dst (destino) nulo e um Gw (gateway) definido
		if route.Dst == nil && route.Gw != nil {
			link, err := netlink.LinkByIndex(route.LinkIndex)
			if err != nil {
				return Route{}, fmt.Errorf("falha ao identificar a interface de rede pelo índice %d: %w", route.LinkIndex, err)
			}

			return Route{
				Gateway:   route.Gw,
				Interface: link.Attrs().Name,
			}, nil
		}
	}

	return Route{}, ErrNoDefaultRoute
}