package cmd

import (
	"context"
	"fmt"
	"netwatch/internal/network"
	"time"

	"github.com/spf13/cobra"
)

// checkCmd representa o comando "check"
var checkCmd = &cobra.Command{
	Use:   "check",
	Short: "Executa diagnóstico completo da rede",
	Long:  `Verifica interface de rede, gateway padrão, latência ICMP, resolução DNS e conectividade TCP.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("NETWATCH CHECK")
		fmt.Println()

		// 1. Interface
		fmt.Println("Interface")
		iface, err := network.GetDefaultInterface()
		if err != nil {
			fmt.Printf("  x Erro: %v\n\n", err)
			return &exitError{code: 2} // 2 = erro de execução/configuração
		}
		fmt.Printf("  ✓ %s\n", iface.Name)
		// Como GetDefaultInterface já filtra por FlagUp, a interface ativa é garantidamente UP.
		fmt.Printf("  ✓ UP\n")
		fmt.Printf("  ✓ IPv4 %s\n\n", iface.IPv4.String())

		// 2. Routing
		fmt.Println("Routing")
		route, err := network.GetDefaultRoute()
		if err != nil {
			fmt.Printf("  x Erro: %v\n\n", err)
			return &exitError{code: 2}
		}
		fmt.Printf("  ✓ Gateway %s\n\n", route.Gateway.String())

		// 3. Connectivity (Paralelo)
		fmt.Println("Connectivity")

		// Contexto com timeout de 2 segundos para evitar travamentos
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		// Estruturas de resultado isoladas para evitar data races lógicas e garantir robustez
		type checkResult struct {
			latency time.Duration
			err     error
		}

		icmpChan := make(chan checkResult, 1)
		dnsChan := make(chan checkResult, 1)
		tcpChan := make(chan checkResult, 1)

		// Goroutine 1: ICMP
		go func() {
			res, err := network.Ping(ctx, route.Gateway.String())
			icmpChan <- checkResult{latency: res.Latency, err: err}
		}()

		// Goroutine 2: DNS
		go func() {
			res, err := network.Resolve(ctx, "google.com")
			dnsChan <- checkResult{latency: res.Latency, err: err}
		}()

		// Goroutine 3: TCP
		go func() {
			latency, err := network.CheckTCP(ctx, "1.1.1.1:443")
			tcpChan <- checkResult{latency: latency, err: err}
		}()

		// Coleta os resultados dos canais
		icmpRes := <-icmpChan
		dnsRes := <-dnsChan
		tcpRes := <-tcpChan

		hasNetError := false

		if icmpRes.err != nil {
			fmt.Printf("  x ICMP       Erro: %v\n", icmpRes.err)
			hasNetError = true
		} else {
			fmt.Printf("  ✓ ICMP       %.1f ms\n", float64(icmpRes.latency.Microseconds())/1000)
		}

		if dnsRes.err != nil {
			fmt.Printf("  x DNS        Erro: %v\n", dnsRes.err)
			hasNetError = true
		} else {
			fmt.Printf("  ✓ DNS        %.1f ms\n", float64(dnsRes.latency.Microseconds())/1000)
		}

		if tcpRes.err != nil {
			fmt.Printf("  x TCP/443    Erro: %v\n", tcpRes.err)
			hasNetError = true
		} else {
			fmt.Printf("  ✓ TCP/443    %.1f ms\n", float64(tcpRes.latency.Microseconds())/1000)
		}

		fmt.Println()

		if hasNetError {
			fmt.Println("Network: DEGRADED / OFFLINE")
			return &exitError{code: 1} // 1 = problema de rede
		}

		fmt.Println("Network: HEALTHY")
		return nil
	},
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
	rootCmd.AddCommand(checkCmd)
}