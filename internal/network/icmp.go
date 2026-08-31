package network

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"net"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
)

// PingResult contém os dados resultantes de um teste ICMP bem-sucedido.
type PingResult struct {
	Latency time.Duration
	TTL     int
}

// generateID gera um ID pseudo-aleatório seguro para o identificador do Echo ICMP,
// prevenindo colisões e race conditions em chamadas paralelas.
func generateID() uint16 {
	var b [2]byte
	_, _ = rand.Read(b[:])
	return binary.BigEndian.Uint16(b[:])
}

// Ping envia um ICMP Echo Request para o host especificado e aguarda a resposta.
// Respeita rigorosamente o ciclo de vida do context.Context (tanto deadlines quanto cancelamentos).
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

	// Configura o deadline base se o contexto possuir um
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}

	// Gera ID único para esta chamada específica (evita race conditions)
	icmpID := generateID()
	icmpSeq := 1

	// Constrói a mensagem Echo Request
	msg := icmp.Message{
		Type: ipv4.ICMPTypeEcho,
		Code: 0,
		Body: &icmp.Echo{
			ID:   int(icmpID),
			Seq:  icmpSeq,
			Data: []byte("netwatch-ping"),
		},
	}
	msgBytes, err := msg.Marshal(nil)
	if err != nil {
		return PingResult{}, fmt.Errorf("falha ao serializar mensagem ICMP: %w", err)
	}

	start := time.Now()

	// Envia o pacote
	_, err = conn.WriteTo(msgBytes, &net.UDPAddr{IP: ipAddr.IP})
	if err != nil {
		return PingResult{}, fmt.Errorf("falha ao enviar pacote ICMP: %w", err)
	}

	// Canal para gerenciar o término assíncrono da leitura e respeitar contextos com cancelamento sem deadline
	type readResult struct {
		n   int
		err error
	}

	reply := make([]byte, 1500)
	readChan := make(chan readResult, 1)

	go func() {
		n, _, readErr := conn.ReadFrom(reply)
		readChan <- readResult{n: n, err: readErr}
	}()

	// Loop de escuta: descarta pacotes espúrios ou de outros IDs até encontrar o nosso ou o contexto estourar
	for {
		if deadline, ok := ctx.Deadline(); ok {
			_ = conn.SetDeadline(deadline)
		}

		select {
		case <-ctx.Done():
			return PingResult{}, fmt.Errorf("ping cancelado ou expirado: %w", ctx.Err())
		case res := <-readChan:
			if res.err != nil {
				return PingResult{}, fmt.Errorf("falha ao receber resposta ICMP: %w", res.err)
			}

			latency := time.Since(start)

			// Faz o parse da mensagem recebida
			rm, err := icmp.ParseMessage(ipv4.ICMPTypeEchoReply.Protocol(), reply[:res.n])
			if err != nil {
				// Se recebermos um pacote malformado ou ruído de rede, continuamos ouvindo se houver tempo
				go func() {
					n, _, readErr := conn.ReadFrom(reply)
					readChan <- readResult{n: n, err: readErr}
				}()
				continue
			}

			switch rm.Type {
			case ipv4.ICMPTypeEchoReply:
				echo, ok := rm.Body.(*icmp.Echo)
				// Valida se o ID e a Sequência correspondem exatamente à nossa requisição
				if !ok || echo.ID != int(icmpID) || echo.Seq != icmpSeq {
					// Pacote ICMP de outro processo/chamada — continua ouvindo
					go func() {
						n, _, readErr := conn.ReadFrom(reply)
						readChan <- readResult{n: n, err: readErr}
					}()
					continue
				}

				return PingResult{
					Latency: latency,
					TTL:     0,
				}, nil

			default:
				// Outros tipos de pacotes ICMP (ex: Destination Unreachable) podem ser tratados ou ignorados no loop
				go func() {
					n, _, readErr := conn.ReadFrom(reply)
					readChan <- readResult{n: n, err: readErr}
				}()
				continue
			}
		}
	}
}