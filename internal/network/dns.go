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
// medindo o tempo gasto na operação através de um resolver puro-Go explícito.
func Resolve(ctx context.Context, domain string) (DNSResult, error) {
	start := time.Now()

	// Utilizamos um net.Resolver dedicado com PreferGo: true para garantir
	// que o comportamento de resolução seja determinístico e independente de CGO/glibc,
	// conversando diretamente com os nameservers configurados no sistema.
	resolver := &net.Resolver{
		PreferGo: true,
	}

	ips, err := resolver.LookupIP(ctx, "ip4", domain)
	if err != nil {
		return DNSResult{}, fmt.Errorf("falha ao resolver DNS para %s: %w", domain, err)
	}

	latency := time.Since(start)

	return DNSResult{
		Addresses: ips,
		Latency:   latency,
	}, nil
}