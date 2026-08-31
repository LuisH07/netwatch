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
// utilizando RouteGet para delegar a escolha da rota padrão ao kernel.
func GetDefaultRoute() (Route, error) {
	// Pergunta ao kernel qual rota ele usaria para alcançar um IP público de referência (1.1.1.1).
	// Isso resolve automaticamente métricas, tabelas de roteamento e políticas do sistema.
	routes, err := netlink.RouteGet(net.ParseIP("1.1.1.1"))
	if err != nil || len(routes) == 0 {
		return Route{}, fmt.Errorf("falha ao determinar rota padrão via netlink: %w", err)
	}

	route := routes[0]
	if route.Gw == nil {
		return Route{}, ErrNoDefaultRoute
	}

	// Obtém a interface associada ao índice retornado pelo kernel na rota
	link, err := netlink.LinkByIndex(route.LinkIndex)
	if err != nil {
		return Route{}, fmt.Errorf("falha ao identificar a interface de rede pelo índice %d: %w", route.LinkIndex, err)
	}

	return Route{
		Gateway:   route.Gw,
		Interface: link.Attrs().Name,
	}, nil
}