package cmd

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/spf13/cobra"
	"netwatch/internal/network"
)

// checkCmd representa o comando "check"
var checkCmd = &cobra.Command{
	Use:   "check",
	Short: "Executa diagnóstico completo da rede",
	Long:  `Verifica interface de rede, gateway padrão, latência ICMP, resolução DNS e conectividade TCP.`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("NETWATCH CHECK")
		fmt.Println()

		// 1. Interface
		fmt.Println("Interface")
		iface, err := network.GetDefaultInterface()
		if err != nil {
			fmt.Printf("  x Erro: %v\n\n", err)
			os.Exit(2) // 2 = erro de execução/configuração
		}
		fmt.Printf("  ✓ %s\n", iface.Name)
		if iface.Up {
			fmt.Printf("  ✓ UP\n")
		} else {
			fmt.Printf("  x DOWN\n")
		}
		fmt.Printf("  ✓ IPv4 %s\n\n", iface.IPv4.String())

		// 2. Routing
		fmt.Println("Routing")
		route, err := network.GetDefaultRoute()
		if err != nil {
			fmt.Printf("  x Erro: %v\n\n", err)
			os.Exit(2)
		}
		fmt.Printf("  ✓ Gateway %s\n\n", route.Gateway.String())

		// 3. Connectivity (Paralelo)
		fmt.Println("Connectivity")

		// Contexto com timeout de 2 segundos para evitar travamentos
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		var wg sync.WaitGroup
		wg.Add(3)

		// Variáveis para armazenar os resultados das goroutines
		var icmpLatency, dnsLatency, tcpLatency time.Duration
		var icmpErr, dnsErr, tcpErr error

		// Goroutine 1: ICMP
		go func() {
			defer wg.Done()
			res, err := network.Ping(ctx, route.Gateway.String())
			icmpLatency = res.Latency
			icmpErr = err
		}()

		// Goroutine 2: DNS
		go func() {
			defer wg.Done()
			res, err := network.Resolve(ctx, "google.com")
			dnsLatency = res.Latency
			dnsErr = err
		}()

		// Goroutine 3: TCP
		go func() {
			defer wg.Done()
			tcpLatency, tcpErr = network.CheckTCP(ctx, "1.1.1.1:443")
		}()

		// Aguarda todas as verificações terminarem
		wg.Wait()

		hasNetError := false

		if icmpErr != nil {
			fmt.Printf("  x ICMP       Erro: %v\n", icmpErr)
			hasNetError = true
		} else {
			fmt.Printf("  ✓ ICMP       %.1f ms\n", float64(icmpLatency.Microseconds())/1000)
		}

		if dnsErr != nil {
			fmt.Printf("  x DNS        Erro: %v\n", dnsErr)
			hasNetError = true
		} else {
			fmt.Printf("  ✓ DNS        %.1f ms\n", float64(dnsLatency.Microseconds())/1000)
		}

		if tcpErr != nil {
			fmt.Printf("  x TCP/443    Erro: %v\n", tcpErr)
			hasNetError = true
		} else {
			fmt.Printf("  ✓ TCP/443    %.1f ms\n", float64(tcpLatency.Microseconds())/1000)
		}

		fmt.Println()

		if hasNetError {
			fmt.Println("Network: DEGRADED / OFFLINE")
			os.Exit(1) // 1 = problema de rede
		}

		fmt.Println("Network: HEALTHY")
		os.Exit(0) // 0 = tudo OK
	},
}

func init() {
	rootCmd.AddCommand(checkCmd)
}