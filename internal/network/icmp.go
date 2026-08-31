package network

import (
	"context"
	"fmt"
	"net"
	"os"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
)

// PingResult contém os dados resultantes de um teste ICMP bem-sucedido.
type PingResult struct {
	Latency time.Duration
	TTL     int
}

// Ping envia um ICMP Echo Request para o host especificado e aguarda a resposta.
// Ele respeita o ciclo de vida do context.Context para evitar travamentos (timeouts).
func Ping(ctx context.Context, host string) (PingResult, error) {
	// Resolve o host (suporta tanto IP direto quanto domínio)
	ipAddr, err := net.ResolveIPAddr("ip4", host)
	if err != nil {
		return PingResult{}, fmt.Errorf("falha ao resolver host: %w", err)
	}

	// Usamos "udp4" para unprivileged ICMP ping (não exige permissão de root)
	conn, err := icmp.ListenPacket("udp4", "0.0.0.0")
	if err != nil {
		return PingResult{}, fmt.Errorf("falha ao abrir socket ICMP (verifique net.ipv4.ping_group_range): %w", err)
	}
	defer conn.Close()

	// Aplica o timeout do contexto no socket de rede
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}

	// Constrói a mensagem Echo Request
	msg := icmp.Message{
		Type: ipv4.ICMPTypeEcho,
		Code: 0,
		Body: &icmp.Echo{
			ID:   os.Getpid() & 0xffff,
			Seq:  1,
			Data: []byte("netwatch-ping"),
		},
	}
	msgBytes, err := msg.Marshal(nil)
	if err != nil {
		return PingResult{}, fmt.Errorf("falha ao serializar mensagem ICMP: %w", err)
	}

	start := time.Now()

	// Envia o pacote (usando UDPAddr devido ao tipo de socket "udp4")
	_, err = conn.WriteTo(msgBytes, &net.UDPAddr{IP: ipAddr.IP})
	if err != nil {
		return PingResult{}, fmt.Errorf("falha ao enviar pacote ICMP: %w", err)
	}

	// Aguarda e lê a resposta
	reply := make([]byte, 1500)
	n, _, err := conn.ReadFrom(reply)
	if err != nil {
		return PingResult{}, fmt.Errorf("falha ao receber resposta ICMP: %w", err)
	}

	latency := time.Since(start)

	// Faz o parse da mensagem de resposta
	rm, err := icmp.ParseMessage(ipv4.ICMPTypeEchoReply.Protocol(), reply[:n])
	if err != nil {
		return PingResult{}, fmt.Errorf("falha ao decodificar a resposta ICMP: %w", err)
	}

	switch rm.Type {
	case ipv4.ICMPTypeEchoReply:
		// Em unprivileged pings, a extração do TTL real exigiria chamadas sys/raw sockets de baixo nível.
		// Para o escopo funcional, a latência é a métrica principal.
		return PingResult{
			Latency: latency,
			TTL:     0,
		}, nil
	default:
		return PingResult{}, fmt.Errorf("pacote ICMP inesperado recebido: %v", rm.Type)
	}
}