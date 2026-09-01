package cmd

import (
	"context"
	"fmt"
	"net"
	"netwatch/internal/network"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

var (
	dnsTarget string
	tcpTarget string
)

// Pontos de injeção substituíveis em testes, para exercitar a orquestração de checkCmd sem
// depender de rede, netlink ou sockets ICMP reais.
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

// checkLine imprime uma linha de diagnóstico com marcador colorido, rótulo alinhado e um
// detalhe adicional (valor medido, endereço resolvido, etc.) para dar contexto real ao
// usuário além do símbolo de sucesso/falha.
func checkLine(ok bool, label, detail string) {
	mark := checkOKMark
	if !ok {
		mark = checkFailMark
	}
	fmt.Printf("  %s %-*s %s\n", mark, checkLabelSize, label, detail)
}

func checkErrorLine(label string, err error) {
	fmt.Printf("  %s %-*s %s\n", checkFailMark, checkLabelSize, label, checkErrText.Render("Erro: "+err.Error()))
}

// checkCmd representa o comando "check"
var checkCmd = &cobra.Command{
	Use:   "check",
	Short: "Executa diagnóstico completo da rede",
	Long:  `Verifica interface de rede, gateway padrão, latência ICMP, resolução DNS e conectividade TCP.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("NETWATCH CHECK")
		fmt.Println()

		// 1. Interface
		fmt.Println(checkSection.Render("Interface"))
		iface, err := getDefaultInterface()
		if err != nil {
			checkErrorLine("Interface padrão", err)
			fmt.Println()
			return &exitError{code: 2} // 2 = erro de execução/configuração
		}
		checkLine(true, "Nome", iface.Name)
		// Como GetDefaultInterface já filtra por FlagUp, a interface ativa é garantidamente UP.
		checkLine(true, "Estado", "UP")
		checkLine(true, "Endereço IPv4", iface.IPv4.String())
		mac := iface.MAC
		if mac == "" {
			mac = checkDetail.Render("(sem endereço MAC, ex.: interface virtual)")
		}
		checkLine(true, "Endereço MAC", mac)
		fmt.Println()

		// 2. Routing
		fmt.Println(checkSection.Render("Routing"))
		route, err := getDefaultRoute()
		if err != nil {
			checkErrorLine("Rota padrão", err)
			fmt.Println()
			return &exitError{code: 2}
		}
		checkLine(true, "Gateway", route.Gateway.String())
		checkLine(true, "Interface de saída", route.Interface)
		fmt.Println()

		// 3. Connectivity (Paralelo)
		fmt.Println(checkSection.Render("Connectivity"))

		// Contexto com timeout de 5 segundos: tempo suficiente para redes domésticas normais
		// sem deixar o comando travado indefinidamente em caso de falha real.
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		type checkResult struct {
			latency   time.Duration
			ttl       int
			addresses []net.IP
			err       error
		}

		icmpChan := make(chan checkResult, 1)
		dnsChan := make(chan checkResult, 1)
		tcpChan := make(chan checkResult, 1)

		// Goroutine 1: ICMP
		go func() {
			res, err := pingFn(ctx, route.Gateway.String())
			icmpChan <- checkResult{latency: res.Latency, ttl: res.TTL, err: err}
		}()

		// Goroutine 2: DNS
		go func() {
			res, err := resolveFn(ctx, dnsTarget)
			dnsChan <- checkResult{latency: res.Latency, addresses: res.Addresses, err: err}
		}()

		// Goroutine 3: TCP
		go func() {
			latency, err := checkTCPFn(ctx, tcpTarget)
			tcpChan <- checkResult{latency: latency, err: err}
		}()

		// Coleta os resultados dos canais
		icmpRes := <-icmpChan
		dnsRes := <-dnsChan
		tcpRes := <-tcpChan

		hasNetError := false

		icmpLabel := fmt.Sprintf("ICMP (%s)", route.Gateway.String())
		if icmpRes.err != nil {
			checkErrorLine(icmpLabel, icmpRes.err)
			hasNetError = true
		} else {
			detail := fmt.Sprintf("%.1f ms", msOf(icmpRes.latency))
			if icmpRes.ttl > 0 {
				detail += checkDetail.Render(fmt.Sprintf("   TTL %d", icmpRes.ttl))
			}
			checkLine(true, icmpLabel, detail)
		}

		dnsLabel := fmt.Sprintf("DNS (%s)", dnsTarget)
		if dnsRes.err != nil {
			checkErrorLine(dnsLabel, dnsRes.err)
			hasNetError = true
		} else {
			detail := fmt.Sprintf("%.1f ms", msOf(dnsRes.latency))
			if len(dnsRes.addresses) > 0 {
				detail += checkDetail.Render("   -> " + joinIPs(dnsRes.addresses))
			}
			checkLine(true, dnsLabel, detail)
		}

		tcpLabel := fmt.Sprintf("TCP (%s)", tcpTarget)
		if tcpRes.err != nil {
			checkErrorLine(tcpLabel, tcpRes.err)
			hasNetError = true
		} else {
			checkLine(true, tcpLabel, fmt.Sprintf("%.1f ms", msOf(tcpRes.latency)))
		}

		fmt.Println()

		if hasNetError {
			fmt.Println(checkDegraded.Render("Network: DEGRADED / OFFLINE"))
			return &exitError{code: 1} // 1 = problema de rede
		}

		fmt.Println(checkHealthy.Render("Network: HEALTHY"))
		return nil
	},
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

// exitError encapsula um código de saída customizado sem matar o processo via os.Exit prematuro,
// permitindo a execução correta de todos os defers da aplicação.
type exitError struct {
	code int
}

func (e *exitError) Error() string {
	return fmt.Sprintf("exit code %d", e.code)
}

func init() {
	checkCmd.Flags().StringVar(&dnsTarget, "dns-target", "google.com", "Host usado para testar a resolução DNS")
	checkCmd.Flags().StringVar(&tcpTarget, "tcp-target", "9.9.9.9:443", "Endereço host:porta usado para testar conectividade TCP")
	rootCmd.AddCommand(checkCmd)
}
