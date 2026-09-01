package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"netwatch/internal/wifi"
)

// wifiSubView identifica em que passo do fluxo de conexão a página Wi-Fi está.
type wifiSubView int

const (
	wifiViewList wifiSubView = iota
	wifiViewPassword
	wifiViewConnecting
	wifiViewResult
)

// apItem adapta wifi.AccessPoint para list.Item, com o badge de "Conectada"/"Conhecida".
type apItem struct {
	ap        wifi.AccessPoint
	connected bool
}

func (i apItem) Title() string {
	if i.connected {
		return i.ap.SSID + "  ● Conectada"
	}
	if i.ap.Known {
		return i.ap.SSID + "  ✓ Conhecida"
	}
	return i.ap.SSID
}

func (i apItem) Description() string {
	sec := "Aberta"
	if i.ap.Secured {
		sec = "WPA/WPA2"
	}
	return fmt.Sprintf("%d%%  •  %s  •  %s", i.ap.Strength, sec, i.ap.BSSID)
}

func (i apItem) FilterValue() string { return i.ap.SSID }

// wifiPageModel é a página "Wi-Fi": lista de Access Points + fluxo de conexão (senha quando
// necessário, spinner de progresso, resultado).
type wifiPageModel struct {
	loaded   bool
	list     list.Model
	scanning bool
	scanErr  error

	sub        wifiSubView
	pwInput    textinput.Model
	targetSSID string

	connecting    bool
	connectGen    int
	connectCancel context.CancelFunc
	connectErr    error
	connectOK     bool
	connectedSSID string
}

// wifiScannedMsg carrega o resultado de um escaneamento de Access Points.
type wifiScannedMsg struct {
	aps []wifi.AccessPoint
	err error
}

// wifiConnectResultMsg carrega o resultado de uma tentativa de conexão. gen identifica a
// tentativa que a originou — usado para descartar resultados de tentativas já
// canceladas/substituídas (ver wifiPageModel.connectGen).
type wifiConnectResultMsg struct {
	ssid string
	gen  int
	err  error
}

func newWifiPageModel() wifiPageModel {
	d := list.NewDefaultDelegate()
	d.Styles.SelectedTitle = lipgloss.NewStyle().
		Foreground(catGreen).
		Bold(true).
		Border(lipgloss.NormalBorder(), false, false, false, true).
		BorderForeground(catGreen).
		PaddingLeft(1)
	d.Styles.SelectedDesc = lipgloss.NewStyle().Foreground(catText).PaddingLeft(2)
	d.Styles.NormalTitle = lipgloss.NewStyle().Foreground(catText).PaddingLeft(2)
	d.Styles.NormalDesc = lipgloss.NewStyle().Foreground(catOverlay0).PaddingLeft(2)

	l := list.New(nil, d, 0, 0)
	l.Title = "Redes Wi-Fi"
	l.Styles.Title = lipgloss.NewStyle().Foreground(catBlue).Bold(true).MarginBottom(1)
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(true)
	l.SetShowHelp(false)
	l.SetShowPagination(false)

	pw := textinput.New()
	pw.EchoMode = textinput.EchoPassword
	pw.EchoCharacter = '•'
	pw.Placeholder = "senha"
	pw.CharLimit = 128

	return wifiPageModel{list: l, pwInput: pw}
}

func scanCmd(mgr wifi.Manager) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		aps, err := mgr.List(ctx)
		return wifiScannedMsg{aps: aps, err: err}
	}
}

func connectCmd(ctx context.Context, mgr wifi.Manager, ssid, password string, gen int) tea.Cmd {
	return func() tea.Msg {
		err := mgr.Connect(ctx, ssid, password)
		return wifiConnectResultMsg{ssid: ssid, gen: gen, err: err}
	}
}

func (m wifiPageModel) isLoading() bool {
	return m.scanning || m.connecting
}

// capturingInput reporta se a página está capturando texto livre (filtro da lista de APs ou
// campo de senha) — usado pelo model de topo para não interceptar teclas globais nesse caso.
func (m wifiPageModel) capturingInput() bool {
	return m.sub == wifiViewPassword || m.list.FilterState() == list.Filtering
}

func (m wifiPageModel) resize(width, height int) wifiPageModel {
	m.list.SetSize(width, height)
	m.pwInput.Width = width
	return m
}

// onEnter dispara o primeiro escaneamento ao visitar a página, uma única vez.
func (m wifiPageModel) onEnter(mgr wifi.Manager) (wifiPageModel, tea.Cmd) {
	if m.loaded || mgr == nil {
		return m, nil
	}
	m.loaded = true
	m.scanning = true
	return m, scanCmd(mgr)
}

func apItemsFromAPs(aps []wifi.AccessPoint, currentSSID string) []list.Item {
	items := make([]list.Item, len(aps))
	for i, ap := range aps {
		items[i] = apItem{ap: ap, connected: currentSSID != "" && ap.SSID == currentSSID}
	}
	return items
}

func (m wifiPageModel) startConnect(mgr wifi.Manager, password string) (wifiPageModel, tea.Cmd) {
	m.pwInput.Blur()
	m.sub = wifiViewConnecting
	m.connecting = true
	m.connectGen++
	gen := m.connectGen
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	m.connectCancel = cancel
	return m, connectCmd(ctx, mgr, m.targetSSID, password, gen)
}

func (m wifiPageModel) Update(msg tea.Msg, mgr wifi.Manager, currentSSID string) (wifiPageModel, tea.Cmd) {
	switch msg := msg.(type) {
	case wifiScannedMsg:
		m.scanning = false
		m.scanErr = msg.err
		if msg.err == nil {
			m.list.SetItems(apItemsFromAPs(msg.aps, currentSSID))
		}
		return m, nil

	case wifiConnectResultMsg:
		if msg.gen != m.connectGen {
			// Resultado de uma tentativa já cancelada/substituída — ignora.
			return m, nil
		}
		m.connecting = false
		m.connectCancel = nil
		m.connectErr = msg.err
		m.connectOK = msg.err == nil
		m.connectedSSID = msg.ssid
		m.sub = wifiViewResult
		return m, nil

	case tea.KeyMsg:
		switch m.sub {
		case wifiViewPassword:
			switch msg.String() {
			case "esc":
				m.sub = wifiViewList
				m.pwInput.Blur()
				return m, nil
			case "enter":
				if strings.TrimSpace(m.pwInput.Value()) == "" {
					return m, nil
				}
				return m.startConnect(mgr, m.pwInput.Value())
			}
			var cmd tea.Cmd
			m.pwInput, cmd = m.pwInput.Update(msg)
			return m, cmd

		case wifiViewConnecting:
			if msg.String() == "esc" && m.connectCancel != nil {
				m.connectCancel()
				m.connectCancel = nil
				m.connecting = false
				m.connectGen++ // invalida qualquer resultado que ainda chegue dessa tentativa
				m.sub = wifiViewList
			}
			return m, nil

		case wifiViewResult:
			switch msg.String() {
			case "enter", "esc":
				m.sub = wifiViewList
				if mgr != nil {
					m.scanning = true
					return m, scanCmd(mgr)
				}
			}
			return m, nil

		default: // wifiViewList
			if m.list.FilterState() == list.Filtering {
				var cmd tea.Cmd
				m.list, cmd = m.list.Update(msg)
				return m, cmd
			}
			switch msg.String() {
			case "r":
				if mgr == nil || m.scanning {
					return m, nil
				}
				m.scanning = true
				m.scanErr = nil
				return m, scanCmd(mgr)
			case "enter":
				item, ok := m.list.SelectedItem().(apItem)
				if !ok || mgr == nil || item.connected {
					return m, nil
				}
				m.targetSSID = item.ap.SSID
				if item.ap.Secured {
					m.sub = wifiViewPassword
					m.pwInput.SetValue("")
					m.pwInput.Focus()
					return m, textinput.Blink
				}
				return m.startConnect(mgr, "")
			}
			var cmd tea.Cmd
			m.list, cmd = m.list.Update(msg)
			return m, cmd
		}

	default:
		// Mensagens internas de componentes (ex.: list.FilterMatchesMsg, que aplica o
		// resultado assíncrono da filtragem, ou o piscar do cursor do textinput de senha)
		// precisam ser repassadas ao componente ativo — sem isso, o filtro da lista nunca
		// chega a estreitar os itens visíveis, mesmo com o texto digitado corretamente.
		switch m.sub {
		case wifiViewList:
			var cmd tea.Cmd
			m.list, cmd = m.list.Update(msg)
			return m, cmd
		case wifiViewPassword:
			var cmd tea.Cmd
			m.pwInput, cmd = m.pwInput.Update(msg)
			return m, cmd
		}
	}
	return m, nil
}

func (m wifiPageModel) View(mgrErr error, spinnerView string) string {
	if mgrErr != nil {
		return checkErrText.Render("Erro ao inicializar NetworkManager: "+mgrErr.Error()) +
			"\n\n" + helpDescStyle.Render("Corrija e reinicie a TUI para tentar novamente.")
	}

	switch m.sub {
	case wifiViewPassword:
		var b strings.Builder
		b.WriteString(fmt.Sprintf("Rede protegida: %s\n\n", m.targetSSID))
		b.WriteString("Senha: ")
		b.WriteString(m.pwInput.View())
		return b.String()

	case wifiViewConnecting:
		return fmt.Sprintf("%s Conectando a %s...", spinnerView, m.targetSSID)

	case wifiViewResult:
		if m.connectOK {
			return checkOKMark + " Conectado a " + m.connectedSSID + " com sucesso!"
		}
		return checkFailMark + " Falha ao conectar a " + m.connectedSSID + ": " + m.connectErr.Error()
	}

	// wifiViewList
	if m.scanning && len(m.list.Items()) == 0 {
		return spinnerView + " Escaneando redes Wi-Fi..."
	}
	var b strings.Builder
	if m.scanning {
		b.WriteString(spinnerView)
		b.WriteString(" Atualizando lista...\n\n")
	}
	if m.scanErr != nil {
		b.WriteString(checkErrText.Render("Erro ao escanear: " + m.scanErr.Error()))
		b.WriteString("\n\n")
	}
	b.WriteString(m.list.View())
	return b.String()
}
