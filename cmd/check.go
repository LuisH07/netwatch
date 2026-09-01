package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var (
	dnsTarget string
	tcpTarget string
)

// checkCmd representa o comando "check"
var checkCmd = &cobra.Command{
	Use:   "check",
	Short: "Executa diagnóstico completo da rede",
	Long:  `Verifica interface de rede, gateway padrão, latência ICMP, resolução DNS e conectividade TCP.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		report := runDiagnostics()
		fmt.Print(renderCheckReport(report))

		if report.InterfaceErr != nil || report.RouteErr != nil {
			return &exitError{code: 2} // 2 = erro de execução/configuração
		}
		if report.Degraded {
			return &exitError{code: 1} // 1 = problema de rede
		}
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
	checkCmd.Flags().StringVar(&dnsTarget, "dns-target", "google.com", "Host usado para testar a resolução DNS")
	checkCmd.Flags().StringVar(&tcpTarget, "tcp-target", "9.9.9.9:443", "Endereço host:porta usado para testar conectividade TCP")
	rootCmd.AddCommand(checkCmd)
}
