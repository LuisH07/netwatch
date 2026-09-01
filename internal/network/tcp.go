package network

import (
	"context"
	"fmt"
	"net"
	"time"
)

// CheckTCP tenta estabelecer uma conexão TCP com o endereço fornecido (ex: "1.1.1.1:443").
// Retorna o tempo decorrido até a confirmação da conexão ou um erro em caso de falha/timeout.
func CheckTCP(ctx context.Context, address string) (time.Duration, error) {
	start := time.Now()

	// O Dialer permite injetar o contexto para respeitar timeouts globais ou cancelamentos.
	dialer := &net.Dialer{}
	conn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return 0, fmt.Errorf("falha ao conectar via TCP no endereço %s: %w", address, err)
	}

	// Fechamos a conexão imediatamente, pois o objetivo é apenas validar o three-way handshake.
	_ = conn.Close()

	return time.Since(start), nil
}
