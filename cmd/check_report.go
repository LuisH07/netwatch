package cmd

import (
	"context"
	"fmt"
	"net"
	"netwatch/internal/network"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// Pontos de injeção substituíveis em testes, para exercitar a orquestração de runDiagnostics
// sem depender de rede, netlink ou sockets ICMP reais.
var (
	getDefaultInterface = network.GetDefaultInterface
	getDefaultRoute     = network.GetDefaultRoute
	pingFn              = network.Ping
	resolveFn           = network.Resolve
	checkTCPFn          = network.CheckTCP
)

var (
	checkOKMark    = lipgloss.NewStyle().Foreground(catGreen).Bold(true).Render("✓")
	checkFailMark  = lipgloss.NewStyle().Foreground(catRed).Bold(true).Render("x")
	checkSection   = lipgloss.NewStyle().Foreground(catBlue).Bold(true)
	checkDetail    = lipgloss.NewStyle().Foreground(catOverlay0)
	checkErrText   = lipgloss.NewStyle().Foreground(catRed)
	checkHealthy   = lipgloss.NewStyle().Foreground(catGreen).Bold(true)
	checkDegraded  = lipgloss.NewStyle().Foreground(catRed).Bold(true)
	checkLabelSize = 24
)

// checkStage contém o resultado de um teste de conectividade individual (ICMP, DNS ou TCP).
type checkStage struct {
	Latency   time.Duration
	TTL       int
	Addresses []net.IP
	Err       error
}

// checkReport é o resultado completo e já calculado de um diagnóstico — separado da
// renderização para que tanto `netwatch check` (CLI) quanto a página de Diagnóstico da TUI
// (`netwatch menu`) usem exatamente a mesma lógica e o mesmo formato de saída, sem duplicação.
type checkReport struct {
	Interface    network.InterfaceInfo
	InterfaceErr error // falha ao obter a interface padrão — interrompe o relatório aqui

	Route    network.Route
	RouteErr error // falha ao obter a rota padrão — interrompe o relatório aqui

	ICMPLabel, DNSLabel, TCPLabel string
	ICMP, DNS, TCP                checkStage

	// Degraded indica um problema de rede (não de execução): interface down ou falha em
	// algum dos testes de conectividade — distinto de InterfaceErr/RouteErr.
	Degraded bool
}

// runDiagnostics executa o diagnóstico completo de rede (interface, rota, ICMP/DNS/TCP em
// paralelo) e retorna o resultado computado, sem imprimir nada — ver renderCheckReport.
func runDiagnostics() checkReport {
	var report checkReport

	iface, err := getDefaultInterface()
	report.Interface = iface
	report.InterfaceErr = err
	if err != nil {
		return report
	}
	// GetDefaultInterface apenas reporta o estado do flag UP do kernel, sem filtrar por ele —
	// uma rota padrão pode, em casos raros (entrada de rota obsoleta, corrida durante a queda
	// da interface), apontar para uma interface que já está down.
	if !iface.Up {
		report.Degraded = true
	}

	route, err := getDefaultRoute()
	report.Route = route
	report.RouteErr = err
	if err != nil {
		return report
	}

	// Contexto com timeout de 5 segundos: tempo suficiente para redes domésticas normais
	// sem deixar o comando travado indefinidamente em caso de falha real.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	icmpChan := make(chan checkStage, 1)
	dnsChan := make(chan checkStage, 1)
	tcpChan := make(chan checkStage, 1)

	go func() {
		res, err := pingFn(ctx, route.Gateway.String())
		icmpChan <- checkStage{Latency: res.Latency, TTL: res.TTL, Err: err}
	}()
	go func() {
		res, err := resolveFn(ctx, dnsTarget)
		dnsChan <- checkStage{Latency: res.Latency, Addresses: res.Addresses, Err: err}
	}()
	go func() {
		latency, err := checkTCPFn(ctx, tcpTarget)
		tcpChan <- checkStage{Latency: latency, Err: err}
	}()

	report.ICMP = <-icmpChan
	report.DNS = <-dnsChan
	report.TCP = <-tcpChan

	report.ICMPLabel = fmt.Sprintf("ICMP (%s)", route.Gateway.String())
	report.DNSLabel = fmt.Sprintf("DNS (%s)", dnsTarget)
	report.TCPLabel = fmt.Sprintf("TCP (%s)", tcpTarget)

	if report.ICMP.Err != nil || report.DNS.Err != nil || report.TCP.Err != nil {
		report.Degraded = true
	}

	return report
}

// formatCheckLine formata uma linha de diagnóstico com marcador colorido, rótulo alinhado e
// um detalhe adicional (valor medido, endereço resolvido, etc.).
func formatCheckLine(ok bool, label, detail string) string {
	mark := checkOKMark
	if !ok {
		mark = checkFailMark
	}
	return fmt.Sprintf("  %s %-*s %s\n", mark, checkLabelSize, label, detail)
}

func formatCheckErrorLine(label string, err error) string {
	return fmt.Sprintf("  %s %-*s %s\n", checkFailMark, checkLabelSize, label, checkErrText.Render("Erro: "+err.Error()))
}

// renderCheckReport formata um checkReport exatamente como `netwatch check` imprime hoje,
// incluindo as duas formas de saída antecipada (falha de interface, falha de rota).
func renderCheckReport(r checkReport) string {
	var b strings.Builder

	b.WriteString("NETWATCH CHECK\n\n")

	b.WriteString(checkSection.Render("Interface"))
	b.WriteString("\n")
	if r.InterfaceErr != nil {
		b.WriteString(formatCheckErrorLine("Interface padrão", r.InterfaceErr))
		b.WriteString("\n")
		return b.String()
	}
	b.WriteString(formatCheckLine(true, "Nome", r.Interface.Name))
	stateLabel := "DOWN"
	if r.Interface.Up {
		stateLabel = "UP"
	}
	b.WriteString(formatCheckLine(r.Interface.Up, "Estado", stateLabel))
	b.WriteString(formatCheckLine(true, "Endereço IPv4", r.Interface.IPv4.String()))
	mac := r.Interface.MAC
	if mac == "" {
		mac = checkDetail.Render("(sem endereço MAC, ex.: interface virtual)")
	}
	b.WriteString(formatCheckLine(true, "Endereço MAC", mac))
	b.WriteString("\n")

	b.WriteString(checkSection.Render("Routing"))
	b.WriteString("\n")
	if r.RouteErr != nil {
		b.WriteString(formatCheckErrorLine("Rota padrão", r.RouteErr))
		b.WriteString("\n")
		return b.String()
	}
	b.WriteString(formatCheckLine(true, "Gateway", r.Route.Gateway.String()))
	b.WriteString(formatCheckLine(true, "Interface de saída", r.Route.Interface))
	b.WriteString("\n")

	b.WriteString(checkSection.Render("Connectivity"))
	b.WriteString("\n")

	if r.ICMP.Err != nil {
		b.WriteString(formatCheckErrorLine(r.ICMPLabel, r.ICMP.Err))
	} else {
		detail := fmt.Sprintf("%.1f ms", msOf(r.ICMP.Latency))
		if r.ICMP.TTL > 0 {
			detail += checkDetail.Render(fmt.Sprintf("   TTL %d", r.ICMP.TTL))
		}
		b.WriteString(formatCheckLine(true, r.ICMPLabel, detail))
	}

	if r.DNS.Err != nil {
		b.WriteString(formatCheckErrorLine(r.DNSLabel, r.DNS.Err))
	} else {
		detail := fmt.Sprintf("%.1f ms", msOf(r.DNS.Latency))
		if len(r.DNS.Addresses) > 0 {
			detail += checkDetail.Render("   -> " + joinIPs(r.DNS.Addresses))
		}
		b.WriteString(formatCheckLine(true, r.DNSLabel, detail))
	}

	if r.TCP.Err != nil {
		b.WriteString(formatCheckErrorLine(r.TCPLabel, r.TCP.Err))
	} else {
		b.WriteString(formatCheckLine(true, r.TCPLabel, fmt.Sprintf("%.1f ms", msOf(r.TCP.Latency))))
	}

	b.WriteString("\n")

	if r.Degraded {
		b.WriteString(checkDegraded.Render("Network: DEGRADED / OFFLINE"))
	} else {
		b.WriteString(checkHealthy.Render("Network: HEALTHY"))
	}
	b.WriteString("\n")

	return b.String()
}

func msOf(d time.Duration) float64 {
	return float64(d.Microseconds()) / 1000
}

func joinIPs(ips []net.IP) string {
	strs := make([]string, len(ips))
	for i, ip := range ips {
		strs[i] = ip.String()
	}
	return strings.Join(strs, ", ")
}
