package cmd

import (
	"os"

	"github.com/spf13/cobra"
)

// rootCmd representa o comando base do NetWatch.
var rootCmd = &cobra.Command{
	Use:   "netwatch",
	Short: "Ferramenta CLI para diagnóstico e gerenciamento de rede",
	Long: `NetWatch é um utilitário de linha de comando para Linux. 
Ele centraliza o diagnóstico rápido de conectividade (IPv4, ICMP, DNS, TCP) 
e o gerenciamento de Wi-Fi sem a necessidade de parsing de processos externos.`,
	// A ausência de um campo Run significa que executar apenas "netwatch" 
	// sem subcomandos exibirá a ajuda (help) padrão do Cobra automaticamente.
}

// Execute adiciona todos os comandos filhos ao comando raiz e executa o parsing das flags.
// Esta função é o principal ponto de entrada chamado pelo main.go.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		// Cobra já imprime o erro por padrão, então apenas saímos com código de falha.
		os.Exit(2) // Utilizando 2 para erro de execução/configuração, conforme definido no projeto
	}
}