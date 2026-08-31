package network

import (
	"context"
	"fmt"
	"net"
	"time"
)

// DNSResult contém os resultados da resolução de um domínio.
type DNSResult struct {
	Addresses []net.IP
	Latency   time.Duration
}

// Resolve verifica a resolução de nomes para um domínio específico,
// medindo o tempo gasto na operação.
func Resolve(ctx context.Context, domain string) (DNSResult, error) {
	start := time.Now()

	// Utilizamos o resolver padrão do sistema (que lê /etc/resolv.conf no Linux)
	// Restringimos a "ip4" para manter o escopo do projeto focado em IPv4.
	ips, err := net.DefaultResolver.LookupIP(ctx, "ip4", domain)
	if err != nil {
		return DNSResult{}, fmt.Errorf("falha ao resolver DNS para %s: %w", domain, err)
	}

	latency := time.Since(start)

	return DNSResult{
		Addresses: ips,
		Latency:   latency,
	}, nil
}