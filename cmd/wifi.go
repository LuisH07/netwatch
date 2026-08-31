package cmd

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"netwatch/internal/wifi"
)

var wifiCmd = &cobra.Command{
	Use:   "wifi",
	Short: "Gerencia redes Wi-Fi",
	Long:  `Lista redes disponíveis, conecta e desconecta de access points via NetworkManager.`,
	Run: func(cmd *cobra.Command, args []string) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		mgr, err := wifi.NewNetworkManager()
		if err != nil {
			fmt.Printf("Erro ao inicializar NetworkManager: %v\n", err)
			os.Exit(2)
		}

		current, err := mgr.Current(ctx)
		if err == nil && current.SSID != "" {
			fmt.Printf("Conectado a: %s (Interface: %s)\n", current.SSID, current.Interface)
		} else {
			fmt.Println("Nenhuma rede Wi-Fi ativa no momento.")
		}

		fmt.Println("\nUse 'netwatch wifi list' para ver as redes disponíveis.")
	},
}

var wifiListCmd = &cobra.Command{
	Use:   "list",
	Short: "Lista redes Wi-Fi disponíveis",
	Run: func(cmd *cobra.Command, args []string) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		mgr, err := wifi.NewNetworkManager()
		if err != nil {
			fmt.Printf("Erro ao inicializar NetworkManager: %v\n", err)
			os.Exit(2)
		}

		aps, err := mgr.List(ctx)
		if err != nil {
			fmt.Printf("Erro ao listar redes: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("%-25s %-10s %-10s %-10s\n", "SSID", "SIGNAL", "SECURITY", "BSSID")
		fmt.Println("-----------------------------------------------------------------")
		for _, ap := range aps {
			sec := "Open"
			if ap.Secured {
				sec = "WPA/WPA2"
			}
			fmt.Printf("%-25s %-d%%        %-10s %-10s\n", ap.SSID, ap.Strength, sec, ap.BSSID)
		}
	},
}

var wifiConnectCmd = &cobra.Command{
	Use:   "connect [SSID]",
	Short: "Conecta a uma rede Wi-Fi",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		ssid := args[0]
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		mgr, err := wifi.NewNetworkManager()
		if err != nil {
			fmt.Printf("Erro ao inicializar NetworkManager: %v\n", err)
			os.Exit(2)
		}

		fmt.Printf("Conectando a %s...\n", ssid)
		err = mgr.Connect(ctx, ssid, "")
		if err != nil {
			fmt.Printf("Erro ao conectar: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Conectado com sucesso!")
	},
}

var wifiDisconnectCmd = &cobra.Command{
	Use:   "disconnect",
	Short: "Desconecta da rede Wi-Fi atual",
	Run: func(cmd *cobra.Command, args []string) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		mgr, err := wifi.NewNetworkManager()
		if err != nil {
			fmt.Printf("Erro ao inicializar NetworkManager: %v\n", err)
			os.Exit(2)
		}

		err = mgr.Disconnect(ctx)
		if err != nil {
			fmt.Printf("Erro ao desconectar: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Desconectado com sucesso.")
	},
}

func init() {
	wifiCmd.AddCommand(wifiListCmd)
	wifiCmd.AddCommand(wifiConnectCmd)
	wifiCmd.AddCommand(wifiDisconnectCmd)
	rootCmd.AddCommand(wifiCmd)
}