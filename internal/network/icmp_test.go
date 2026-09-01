package network

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
)

// fakePingConn é uma implementação em memória de pingConn, controlada por um canal de
// pacotes a "entregar", permitindo testar ping() sem um socket ICMP real.
type fakePingConn struct {
	mu       sync.Mutex
	packets  chan fakePacket
	closeCh  chan struct{}
	closed   bool
	closeErr error
	writeErr error
	localID  int
}

type fakePacket struct {
	data []byte
	ttl  int
	err  error
}

func newFakePingConn(localID int) *fakePingConn {
	return &fakePingConn{
		packets: make(chan fakePacket, 8),
		closeCh: make(chan struct{}),
		localID: localID,
	}
}

func (f *fakePingConn) WriteTo(b []byte, addr net.Addr) (int, error) {
	if f.writeErr != nil {
		return 0, f.writeErr
	}
	return len(b), nil
}

func (f *fakePingConn) ReadPacket() ([]byte, int, error) {
	select {
	case p := <-f.packets:
		return p.data, p.ttl, p.err
	case <-f.closeCh:
		// Simula um ReadFrom que retorna erro após o socket ser fechado.
		return nil, 0, net.ErrClosed
	}
}

func (f *fakePingConn) SetDeadline(t time.Time) error { return nil }

func (f *fakePingConn) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.closed {
		f.closed = true
		close(f.closeCh)
	}
	return f.closeErr
}

func (f *fakePingConn) LocalPort() int { return f.localID }

// deliver enfileira um pacote fake para ser "recebido" pela próxima chamada de ReadPacket.
func (f *fakePingConn) deliver(p fakePacket) {
	f.packets <- p
}

// buildEchoReply monta os bytes de uma resposta ICMP Echo Reply válida com o ID/Seq dados.
func buildEchoReply(t *testing.T, id, seq int, data []byte) []byte {
	t.Helper()
	msg := icmp.Message{
		Type: ipv4.ICMPTypeEchoReply,
		Code: 0,
		Body: &icmp.Echo{ID: id, Seq: seq, Data: data},
	}
	b, err := msg.Marshal(nil)
	if err != nil {
		t.Fatalf("failed to build fake echo reply: %v", err)
	}
	return b
}

func TestPing_Success(t *testing.T) {
	const localPort = 40001
	conn := newFakePingConn(localPort)
	conn.deliver(fakePacket{data: buildEchoReply(t, localPort, 1, []byte("netwatch-ping")), ttl: 57})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	res, err := ping(ctx, conn, &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if res.TTL != 57 {
		t.Errorf("TTL = %d, want 57", res.TTL)
	}
	if res.Latency <= 0 {
		t.Errorf("Latency = %v, want > 0", res.Latency)
	}
}

func TestPing_DiscardsSpuriousPacketThenAccepts(t *testing.T) {
	const localPort = 40002
	conn := newFakePingConn(localPort)

	// Pacote de outro processo/ID — deve ser descartado.
	conn.deliver(fakePacket{data: buildEchoReply(t, 9999, 1, []byte("other"))})
	// Pacote malformado — também deve ser descartado sem derrubar o loop.
	conn.deliver(fakePacket{data: []byte{0xFF}})
	// Pacote correto.
	conn.deliver(fakePacket{data: buildEchoReply(t, localPort, 1, []byte("netwatch-ping")), ttl: 12})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	res, err := ping(ctx, conn, &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatalf("expected success after discarding spurious packets, got error: %v", err)
	}
	if res.TTL != 12 {
		t.Errorf("TTL = %d, want 12", res.TTL)
	}
}

func TestPing_ContextCanceledBeforeReply(t *testing.T) {
	conn := newFakePingConn(40003)
	// Nenhum pacote é entregue — o contexto deve expirar primeiro.

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := ping(ctx, conn, &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected wrapped context.DeadlineExceeded, got: %v", err)
	}

	conn.mu.Lock()
	closed := conn.closed
	conn.mu.Unlock()
	if !closed {
		t.Error("expected conn.Close() to have been called via defer")
	}
}

func TestPing_ReadError(t *testing.T) {
	conn := newFakePingConn(40004)
	conn.deliver(fakePacket{err: errors.New("boom")})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err := ping(ctx, conn, &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err == nil {
		t.Fatal("expected error from failed read, got nil")
	}
}

func TestPing_WriteError(t *testing.T) {
	conn := newFakePingConn(40005)
	conn.writeErr = errors.New("network down")

	_, err := ping(context.Background(), conn, &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err == nil {
		t.Fatal("expected error from failed write, got nil")
	}
}

func TestPing_InvalidHost(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	_, err := Ping(ctx, "%invalid%host%")
	if err == nil {
		t.Fatal("expected error resolving an invalid host, got nil")
	}
}

// TestPing_NoGoroutineLeak garante que a goroutine leitora de ping() encerra ao expirar o
// contexto, mesmo sem nunca ter recebido um pacote — regressão do vazamento corrigido em
// internal/network/icmp.go.
func TestPing_NoGoroutineLeak(t *testing.T) {
	conn := newFakePingConn(40006)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	done := make(chan struct{})
	go func() {
		_, _ = ping(ctx, conn, &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("ping() did not return after context expired")
	}
}

// TestICMPConn_RealSocket exercita a implementação real de pingConn (icmpConn) contra um
// socket ICMP não privilegiado de verdade, fazendo um ping de loopback. Pula
// automaticamente em ambientes sem suporte a "ping sockets" (net.ipv4.ping_group_range),
// como certos containers de CI restritos, para não quebrar builds nesses ambientes.
func TestICMPConn_RealSocket(t *testing.T) {
	conn, err := newICMPConn()
	if err != nil {
		t.Skipf("unprivileged ICMP socket not available in this environment: %v", err)
	}

	if conn.LocalPort() == 0 {
		t.Error("expected a non-zero local port for the ICMP socket")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	res, err := ping(ctx, conn, &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatalf("expected successful loopback ping, got error: %v", err)
	}
	if res.Latency <= 0 {
		t.Errorf("Latency = %v, want > 0", res.Latency)
	}
}

// TestPing_RealLoopback cobre o caminho público Ping() de ponta a ponta contra 127.0.0.1,
// incluindo a resolução do host e a criação do socket real. Também pula quando o ambiente
// não permite sockets ICMP não privilegiados.
func TestPing_RealLoopback(t *testing.T) {
	if _, err := newICMPConn(); err != nil {
		t.Skipf("unprivileged ICMP socket not available in this environment: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	res, err := Ping(ctx, "127.0.0.1")
	if err != nil {
		t.Fatalf("expected successful loopback ping, got error: %v", err)
	}
	if res.Latency <= 0 {
		t.Errorf("Latency = %v, want > 0", res.Latency)
	}
}
