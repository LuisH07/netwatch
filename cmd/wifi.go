package cmd

import (
	"context"
	"errors"
	"fmt"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"
	"netwatch/internal/wifi"
)

// Pontos de injeção substituíveis em testes, para exercitar os comandos "wifi" com um
// wifi.Manager fake e sem depender de um terminal real para leitura de senha.
var (
	newManager   = func() (wifi.Manager, error) { return wifi.NewNetworkManager() }
	readPassword = func() (string, error) {
		b, err := term.ReadPassword(int(syscall.Stdin))
		return string(b), err
	}
)

var wifiCmd = &cobra.Command{
	Use:   "wifi",
	Short: "Gerencia redes Wi-Fi",
	Long:  `Lista redes disponíveis, conecta e desconecta de access points via NetworkManager.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		mgr, err := newManager()
		if err != nil {
			fmt.Printf("Erro ao inicializar NetworkManager: %v\n", err)
			return &exitError{code: 2}
		}

		current, err := mgr.Current(ctx)
		if err != nil {
			// Distingue erro real de D-Bus de simplesmente não estar conectado a nenhuma rede
			if errors.Is(err, wifi.ErrNoActiveConnection) {
				fmt.Println("Nenhuma rede Wi-Fi ativa no momento.")
			} else {
				fmt.Printf("Erro ao consultar conexão Wi-Fi: %v\n", err)
				return &exitError{code: 2}
			}
		} else if current.SSID != "" {
			fmt.Printf("Conectado a: %s (Interface: %s)\n", current.SSID, current.Interface)
		}

		fmt.Println("\nUse 'netwatch wifi list' para ver as redes disponíveis.")
		return nil
	},
}

var wifiListCmd = &cobra.Command{
	Use:   "list",
	Short: "Lista redes Wi-Fi disponíveis",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		mgr, err := newManager()
		if err != nil {
			fmt.Printf("Erro ao inicializar NetworkManager: %v\n", err)
			return &exitError{code: 2}
		}

		aps, err := mgr.List(ctx)
		if err != nil {
			fmt.Printf("Erro ao listar redes: %v\n", err)
			return &exitError{code: 1}
		}

		fmt.Printf("%-25s %-10s %-10s %-10s\n", "SSID", "SIGNAL", "SECURITY", "BSSID")
		fmt.Println("-----------------------------------------------------------------")
		for _, ap := range aps {
			sec := "Open"
			if ap.Secured {
				sec = "WPA/WPA2"
			}
			// Correção do verbo de formatação: %-3d alinha perfeitamente valores de 1 a 3 dígitos
			fmt.Printf("%-25s %-3d%%       %-10s %-10s\n", ap.SSID, ap.Strength, sec, ap.BSSID)
		}
		return nil
	},
}

var wifiConnectCmd = &cobra.Command{
	Use:   "connect [SSID]",
	Short: "Conecta a uma rede Wi-Fi",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ssid := args[0]

		mgr, err := newManager()
		if err != nil {
			fmt.Printf("Erro ao inicializar NetworkManager: %v\n", err)
			return &exitError{code: 2}
		}

		// Verifica se a rede exige senha através da listagem rápida de APs, com seu próprio
		// prazo curto — não pode compartilhar o mesmo contexto usado depois para aguardar a
		// conexão, senão o tempo já gasto aqui seria descontado do orçamento de Connect().
		listCtx, listCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer listCancel()
		aps, err := mgr.List(listCtx)
		var isSecured bool
		if err == nil {
			for _, ap := range aps {
				if ap.SSID == ssid {
					isSecured = ap.Secured
					break
				}
			}
		}

		var password string
		if isSecured {
			fmt.Printf("A rede '%s' é protegida. Digite a senha: ", ssid)
			pw, err := readPassword()
			fmt.Println() // Quebra de linha após o input oculto
			if err != nil {
				fmt.Printf("Erro ao ler senha: %v\n", err)
				return &exitError{code: 1}
			}
			password = pw
		}

		fmt.Printf("Conectando a %s...\n", ssid)
		// 45s: handshake WPA + DHCP em redes domésticas mais lentas costuma passar de 15s —
		// um prazo curto demais faz o NetworkManager continuar tentando em segundo plano e
		// terminar de conectar segundos depois de já termos reportado timeout ao usuário.
		connectCtx, connectCancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer connectCancel()
		err = mgr.Connect(connectCtx, ssid, password)
		if err != nil {
			fmt.Printf("Erro ao conectar: %v\n", err)
			return &exitError{code: 1}
		}
		fmt.Println("Conectado com sucesso!")
		return nil
	},
}

var wifiDisconnectCmd = &cobra.Command{
	Use:   "disconnect",
	Short: "Desconecta da rede Wi-Fi atual",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		mgr, err := newManager()
		if err != nil {
			fmt.Printf("Erro ao inicializar NetworkManager: %v\n", err)
			return &exitError{code: 2}
		}

		err = mgr.Disconnect(ctx)
		if err != nil {
			fmt.Printf("Erro ao desconectar: %v\n", err)
			return &exitError{code: 1}
		}
		fmt.Println("Desconectado com sucesso.")
		return nil
	},
}

func init() {
	wifiCmd.AddCommand(wifiListCmd)
	wifiCmd.AddCommand(wifiConnectCmd)
	wifiCmd.AddCommand(wifiDisconnectCmd)
	rootCmd.AddCommand(wifiCmd)
}
