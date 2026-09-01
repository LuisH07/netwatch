package cmd

import (
	"context"
	"errors"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"netwatch/internal/wifi"
)

// statusPageModel é a página "Status": mostra a conexão Wi-Fi atual e permite desconectar.
type statusPageModel struct {
	disconnecting bool
	disconnectErr error
}

func newStatusPageModel() statusPageModel {
	return statusPageModel{}
}

// statusDisconnectDoneMsg carrega o resultado de um pedido de desconexão.
type statusDisconnectDoneMsg struct{ err error }

func disconnectCmd(mgr wifi.Manager) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return statusDisconnectDoneMsg{err: mgr.Disconnect(ctx)}
	}
}

func (m statusPageModel) resize(width, height int) statusPageModel {
	return m
}

func (m statusPageModel) handleDisconnectDone(msg statusDisconnectDoneMsg) (statusPageModel, tea.Cmd) {
	m.disconnecting = false
	m.disconnectErr = msg.err
	return m, nil
}

// Update trata teclas específicas da página Status. mgr pode ser nil enquanto a
// inicialização do wifi.Manager ainda não terminou, ou se ela falhou.
func (m statusPageModel) Update(msg tea.Msg, mgr wifi.Manager, conn wifi.Connection) (statusPageModel, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch keyMsg.String() {
	case "r":
		if mgr == nil {
			return m, nil
		}
		return m, fetchCurrentConnCmd(mgr)
	case "d":
		if mgr == nil || m.disconnecting || conn.SSID == "" {
			return m, nil
		}
		m.disconnecting = true
		m.disconnectErr = nil
		return m, disconnectCmd(mgr)
	}
	return m, nil
}

func (m statusPageModel) View(conn wifi.Connection, connErr error, mgrErr error, spinnerView string) string {
	var b strings.Builder

	if mgrErr != nil {
		b.WriteString(checkErrText.Render("Erro ao inicializar NetworkManager: " + mgrErr.Error()))
		return b.String()
	}

	if m.disconnecting {
		b.WriteString(spinnerView)
		b.WriteString(" Desconectando...")
		return b.String()
	}

	switch {
	case connErr != nil && errors.Is(connErr, wifi.ErrNoActiveConnection):
		b.WriteString(helpDescStyle.Render("Nenhuma rede Wi-Fi ativa no momento."))
	case connErr != nil:
		b.WriteString(checkErrText.Render("Erro ao consultar conexão: " + connErr.Error()))
	case conn.SSID == "":
		b.WriteString(spinnerView)
		b.WriteString(" Carregando status da conexão...")
	default:
		b.WriteString(formatCheckLine(true, "Rede", conn.SSID))
		ip := "-"
		if conn.IPv4 != nil {
			ip = conn.IPv4.String()
		}
		b.WriteString(formatCheckLine(true, "Interface", conn.Interface))
		b.WriteString(formatCheckLine(true, "Endereço IPv4", ip))
	}

	if m.disconnectErr != nil {
		b.WriteString("\n")
		b.WriteString(checkErrText.Render("Erro ao desconectar: " + m.disconnectErr.Error()))
	}

	return b.String()
}
