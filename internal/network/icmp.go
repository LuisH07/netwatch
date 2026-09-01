package network

import (
	"context"
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

// pingConn abstrai o subconjunto do socket ICMP usado por ping, permitindo injetar
// uma implementação fake em testes sem depender de um socket ICMP real (privilegiado
// ou não) disponível no ambiente de execução.
type pingConn interface {
	WriteTo(b []byte, addr net.Addr) (int, error)
	// ReadPacket bloqueia até receber um pacote (ou até o socket ser fechado/expirar),
	// retornando os bytes brutos recebidos e o TTL do cabeçalho IP, quando disponível.
	ReadPacket() (data []byte, ttl int, err error)
	SetDeadline(t time.Time) error
	Close() error
	// LocalPort retorna a porta local do socket, usada como identificador do Echo ICMP
	// (ver comentário em icmpConn.LocalPort na implementação real).
	LocalPort() int
}

// icmpConn é a implementação real de pingConn sobre um socket ICMP "udp4" (não privilegiado).
type icmpConn struct {
	conn *icmp.PacketConn
	p4   *ipv4.PacketConn
}

func newICMPConn() (*icmpConn, error) {
	// Usamos "udp4" para unprivileged ICMP ping (não exige permissão de root)
	conn, err := icmp.ListenPacket("udp4", "0.0.0.0")
	if err != nil {
		return nil, fmt.Errorf("falha ao abrir socket ICMP (verifique net.ipv4.ping_group_range): %w", err)
	}

	// Habilita o recebimento do TTL do cabeçalho IP via ancillary data (IP_RECVTTL),
	// disponível mesmo em sockets ICMP não privilegiados no Linux.
	p4 := conn.IPv4PacketConn()
	if p4 != nil {
		_ = p4.SetControlMessage(ipv4.FlagTTL, true)
	}

	return &icmpConn{conn: conn, p4: p4}, nil
}

func (c *icmpConn) WriteTo(b []byte, addr net.Addr) (int, error) { return c.conn.WriteTo(b, addr) }
func (c *icmpConn) SetDeadline(t time.Time) error                { return c.conn.SetDeadline(t) }
func (c *icmpConn) Close() error                                 { return c.conn.Close() }

// LocalPort retorna a porta local do socket. Em sockets ICMP não privilegiados ("ping
// sockets") do Linux, o kernel reescreve o campo ID do Echo Request para a porta local do
// socket antes de enviar (usada para demultiplexar a resposta de volta a este processo) —
// por isso é essa porta, e não um ID gerado pelo cliente, que deve ser usada para validar
// a resposta recebida.
func (c *icmpConn) LocalPort() int {
	if udpAddr, ok := c.conn.LocalAddr().(*net.UDPAddr); ok {
		return udpAddr.Port
	}
	return 0
}

func (c *icmpConn) ReadPacket() ([]byte, int, error) {
	buf := make([]byte, 1500)
	if c.p4 != nil {
		n, cm, _, err := c.p4.ReadFrom(buf)
		ttl := 0
		if cm != nil {
			ttl = cm.TTL
		}
		return buf[:n], ttl, err
	}
	n, _, err := c.conn.ReadFrom(buf)
	return buf[:n], 0, err
}

// Ping envia um ICMP Echo Request para o host especificado e aguarda a resposta.
// Respeita rigorosamente o ciclo de vida do context.Context (tanto deadlines quanto cancelamentos).
func Ping(ctx context.Context, host string) (PingResult, error) {
	// Resolve o host (suporta tanto IP direto quanto domínio)
	ipAddr, err := net.ResolveIPAddr("ip4", host)
	if err != nil {
		return PingResult{}, fmt.Errorf("falha ao resolver host: %w", err)
	}

	conn, err := newICMPConn()
	if err != nil {
		return PingResult{}, err
	}

	return ping(ctx, conn, &net.UDPAddr{IP: ipAddr.IP})
}

// ping contém a lógica de envio/escuta do Echo ICMP, desacoplada da criação do socket real
// para poder ser testada com uma pingConn fake.
func ping(ctx context.Context, conn pingConn, dst net.Addr) (PingResult, error) {
	defer conn.Close()

	// Configura o deadline base se o contexto possuir um
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}

	icmpID := conn.LocalPort()
	icmpSeq := 1

	// Constrói a mensagem Echo Request
	msg := icmp.Message{
		Type: ipv4.ICMPTypeEcho,
		Code: 0,
		Body: &icmp.Echo{
			ID:   icmpID,
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
	_, err = conn.WriteTo(msgBytes, dst)
	if err != nil {
		return PingResult{}, fmt.Errorf("falha ao enviar pacote ICMP: %w", err)
	}

	// Canal para gerenciar o término assíncrono da leitura e respeitar contextos com cancelamento sem deadline
	type readResult struct {
		data []byte
		ttl  int
		err  error
	}

	readChan := make(chan readResult)

	// Uma única goroutine leitora de longa duração: evita disparar uma goroutine nova
	// por pacote espúrio, o que poderia vazar leituras bloqueadas em ReadPacket indefinidamente.
	// Ela encerra sozinha quando ReadPacket retorna erro (ex.: após conn.Close() no defer acima)
	// ou quando o contexto expira/é cancelado.
	go func() {
		for {
			data, ttl, readErr := conn.ReadPacket()

			select {
			case readChan <- readResult{data: data, ttl: ttl, err: readErr}:
				if readErr != nil {
					return
				}
			case <-ctx.Done():
				return
			}
		}
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
			rm, err := icmp.ParseMessage(ipv4.ICMPTypeEchoReply.Protocol(), res.data)
			if err != nil {
				// Se recebermos um pacote malformado ou ruído de rede, continuamos ouvindo se houver tempo
				continue
			}

			switch rm.Type {
			case ipv4.ICMPTypeEchoReply:
				echo, ok := rm.Body.(*icmp.Echo)
				// Valida se o ID e a Sequência correspondem exatamente à nossa requisição
				if !ok || echo.ID != icmpID || echo.Seq != icmpSeq {
					// Pacote ICMP de outro processo/chamada — continua ouvindo
					continue
				}

				return PingResult{
					Latency: latency,
					TTL:     res.ttl,
				}, nil

			default:
				// Outros tipos de pacotes ICMP (ex: Destination Unreachable) podem ser tratados ou ignorados no loop
				continue
			}
		}
	}
}
