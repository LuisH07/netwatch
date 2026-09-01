package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
	"netwatch/internal/wifi"
)

// Cores Oficiais Catppuccin Mocha
var (
	catBase     = lipgloss.Color("#1e1e2e")
	catSurface0 = lipgloss.Color("#313244")
	catSurface1 = lipgloss.Color("#45475a")
	catOverlay0 = lipgloss.Color("#6c7086")
	catText     = lipgloss.Color("#cdd6f4")
	catLavender = lipgloss.Color("#b4befe")
	catGreen    = lipgloss.Color("#a6e3a1")
	catMauve    = lipgloss.Color("#cba6f7")
	catBlue     = lipgloss.Color("#89b4fa")
	catRed      = lipgloss.Color("#f38ba8")
	catYellow   = lipgloss.Color("#f9e2af")
)

// Estilos de Componentes
var (
	badgeStyle = lipgloss.NewStyle().
			Background(catLavender).
			Foreground(catBase).
			Bold(true).
			Padding(0, 1)

	subHeaderStyle = lipgloss.NewStyle().
			Foreground(catOverlay0).
			MarginLeft(1)

	panelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(catSurface1).
			Padding(1, 2)

	spinnerStyle = lipgloss.NewStyle().
			Foreground(catYellow)

	helpKeyStyle = lipgloss.NewStyle().
			Foreground(catBlue).
			Bold(true)

	helpDescStyle = lipgloss.NewStyle().
			Foreground(catOverlay0)

	tabActiveStyle = lipgloss.NewStyle().
			Foreground(catBase).
			Background(catBlue).
			Bold(true).
			Padding(0, 2)

	tabInactiveStyle = lipgloss.NewStyle().
				Foreground(catOverlay0).
				Padding(0, 2)
)

// newSpinner constrói o spinner reusado por qualquer página que precise indicar uma operação
// em andamento (scan, connect, disconnect, diagnóstico).
func newSpinner() spinner.Model {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = spinnerStyle
	return s
}

// menuChromeHeight é o número de linhas fixas consumidas por tudo que não é conteúdo da
// página ativa: cabeçalho, barra de abas, bordas/padding do painel e o rodapé.
const menuChromeHeight = 12

// page identifica uma das páginas navegáveis da TUI, trocáveis a qualquer momento via
// Tab/Shift+Tab ou pelos atalhos numéricos 1/2/3.
type page int

const (
	pageStatus page = iota
	pageWifi
	pageDiag
)

var pageOrder = []page{pageStatus, pageWifi, pageDiag}

func (p page) title() string {
	switch p {
	case pageStatus:
		return "Status"
	case pageWifi:
		return "Wi-Fi"
	case pageDiag:
		return "Diagnóstico"
	}
	return "?"
}

func (p page) next() page {
	for i, cur := range pageOrder {
		if cur == p {
			return pageOrder[(i+1)%len(pageOrder)]
		}
	}
	return pageStatus
}

func (p page) prev() page {
	for i, cur := range pageOrder {
		if cur == p {
			return pageOrder[(i-1+len(pageOrder))%len(pageOrder)]
		}
	}
	return pageStatus
}

// managerReadyMsg carrega o resultado (uma única vez, disparado em Init) da inicialização do
// wifi.Manager compartilhado por todas as páginas.
type managerReadyMsg struct {
	mgr wifi.Manager
	err error
}

// currentConnMsg carrega o resultado de uma consulta à conexão Wi-Fi atual — estado
// compartilhado entre as páginas Status e Wi-Fi (ex.: para o badge "Conectada" na lista de
// APs), evitando duas fontes de verdade divergentes.
type currentConnMsg struct {
	conn wifi.Connection
	err  error
}

func initManagerCmd() tea.Cmd {
	return func() tea.Msg {
		mgr, err := newManager()
		return managerReadyMsg{mgr: mgr, err: err}
	}
}

func fetchCurrentConnCmd(mgr wifi.Manager) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		conn, err := mgr.Current(ctx)
		return currentConnMsg{conn: conn, err: err}
	}
}

type model struct {
	page   page
	width  int
	height int

	spinner       spinner.Model
	spinnerActive bool

	mgr    wifi.Manager
	mgrErr error

	currentConn       wifi.Connection
	currentConnErr    error
	currentConnLoaded bool

	status statusPageModel
	wifi   wifiPageModel
	diag   diagPageModel
}

var menuCmd = &cobra.Command{
	Use:   "menu",
	Short: "Painel interativo profissional do NetWatch",
	Run: func(cmd *cobra.Command, args []string) {
		m := model{
			spinner: newSpinner(),
			status:  newStatusPageModel(),
			wifi:    newWifiPageModel(),
			diag:    newDiagPageModel(),
		}

		p := tea.NewProgram(m, tea.WithAltScreen())
		if _, err := p.Run(); err != nil {
			fmt.Printf("Erro ao rodar TUI: %v\n", err)
			os.Exit(1)
		}
	},
}

func init() {
	rootCmd.AddCommand(menuCmd)
}

func (m model) Init() tea.Cmd {
	return initManagerCmd()
}

// capturingInput reporta se a página ativa está capturando entrada de texto livre (filtro de
// lista ou campo de senha) — nesse caso, teclas globais (trocar de aba, sair com "q", dígitos)
// não podem ser interceptadas, senão digitar normalmente ficaria impossível.
func (m model) capturingInput() bool {
	return m.page == pageWifi && m.wifi.capturingInput()
}

// isLoading reporta se alguma operação em segundo plano está em andamento em qualquer
// página, para manter o spinner animando independentemente de qual página está ativa no
// momento (ex.: trocar de aba durante um scan não deve parar a animação).
func (m model) isLoading() bool {
	initialLoad := !m.currentConnLoaded && m.mgrErr == nil
	return initialLoad || m.status.disconnecting || m.wifi.isLoading() || m.diag.running
}

// onPageEntered dispara a primeira carga automática de uma página (scan de Wi-Fi,
// diagnóstico), se ainda não tiver ocorrido, ao entrar/trocar para ela.
func (m *model) onPageEntered() tea.Cmd {
	switch m.page {
	case pageWifi:
		var cmd tea.Cmd
		m.wifi, cmd = m.wifi.onEnter(m.mgr)
		return cmd
	case pageDiag:
		var cmd tea.Cmd
		m.diag, cmd = m.diag.onEnter()
		return cmd
	}
	return nil
}

// dispatchToPage repassa msg para o Update da página atualmente ativa.
func (m *model) dispatchToPage(msg tea.Msg) tea.Cmd {
	switch m.page {
	case pageStatus:
		var cmd tea.Cmd
		m.status, cmd = m.status.Update(msg, m.mgr, m.currentConn)
		return cmd
	case pageWifi:
		var cmd tea.Cmd
		m.wifi, cmd = m.wifi.Update(msg, m.mgr, m.currentConn.SSID)
		return cmd
	case pageDiag:
		var cmd tea.Cmd
		m.diag, cmd = m.diag.Update(msg)
		return cmd
	}
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok && keyMsg.String() == "ctrl+c" {
		return m, tea.Quit
	}

	var cmd tea.Cmd

	switch msg := msg.(type) {
	case managerReadyMsg:
		m.mgr = msg.mgr
		m.mgrErr = msg.err
		if msg.err == nil {
			cmd = fetchCurrentConnCmd(m.mgr)
		}

	case currentConnMsg:
		m.currentConn = msg.conn
		m.currentConnErr = msg.err
		m.currentConnLoaded = true

	case statusDisconnectDoneMsg:
		m.status, cmd = m.status.handleDisconnectDone(msg)
		if msg.err == nil && m.mgr != nil {
			cmd = tea.Batch(cmd, fetchCurrentConnCmd(m.mgr))
		}

	case wifiScannedMsg:
		m.wifi, cmd = m.wifi.Update(msg, m.mgr, m.currentConn.SSID)

	case wifiConnectResultMsg:
		wasCurrentGen := msg.gen == m.wifi.connectGen
		m.wifi, cmd = m.wifi.Update(msg, m.mgr, m.currentConn.SSID)
		if wasCurrentGen && msg.err == nil && m.mgr != nil {
			cmd = tea.Batch(cmd, fetchCurrentConnCmd(m.mgr))
		}

	case diagRanMsg:
		m.diag, cmd = m.diag.Update(msg)

	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		contentW := msg.Width - 4
		contentH := msg.Height - menuChromeHeight
		if contentH < 3 {
			contentH = 3
		}
		m.status = m.status.resize(contentW, contentH)
		m.wifi = m.wifi.resize(contentW, contentH)
		m.diag = m.diag.resize(contentW, contentH)

	case spinner.TickMsg:
		if !m.isLoading() {
			m.spinnerActive = false
		} else {
			m.spinner, cmd = m.spinner.Update(msg)
		}

	case tea.KeyMsg:
		if !m.capturingInput() {
			switch msg.String() {
			case "q":
				return m, tea.Quit
			case "tab":
				m.page = m.page.next()
				cmd = m.onPageEntered()
			case "shift+tab":
				m.page = m.page.prev()
				cmd = m.onPageEntered()
			case "1":
				m.page = pageStatus
			case "2":
				m.page = pageWifi
				cmd = m.onPageEntered()
			case "3":
				m.page = pageDiag
				cmd = m.onPageEntered()
			default:
				cmd = m.dispatchToPage(msg)
			}
		} else {
			cmd = m.dispatchToPage(msg)
		}

	default:
		// Outras mensagens (ex.: piscar do cursor do textinput) — repassa para a página ativa.
		cmd = m.dispatchToPage(msg)
	}

	if m.isLoading() && !m.spinnerActive {
		m.spinnerActive = true
		cmd = tea.Batch(cmd, spinner.Tick)
	}
	return m, cmd
}

func (m model) View() string {
	var s strings.Builder

	s.WriteString(badgeStyle.Render(" NETWATCH "))
	s.WriteString(subHeaderStyle.Render("Network Manager Console"))
	s.WriteString("\n\n")

	for _, p := range pageOrder {
		style := tabInactiveStyle
		if p == m.page {
			style = tabActiveStyle
		}
		s.WriteString(style.Render(p.title()))
		s.WriteString(" ")
	}
	s.WriteString("\n\n")

	spinnerView := m.spinner.View()

	var content string
	switch m.page {
	case pageStatus:
		content = m.status.View(m.currentConn, m.currentConnErr, m.mgrErr, spinnerView)
	case pageWifi:
		content = m.wifi.View(m.mgrErr, spinnerView)
	case pageDiag:
		content = m.diag.View(spinnerView)
	}

	panel := panelStyle
	if m.width > 0 {
		panel = panel.Width(m.width - 4)
	}
	s.WriteString(panel.Render(content))
	s.WriteString("\n\n")

	s.WriteString(m.footerHints())

	return s.String()
}

func (m model) footerHints() string {
	var parts []string
	addHint := func(key, desc string) {
		parts = append(parts, helpKeyStyle.Render(key)+helpDescStyle.Render(" "+desc))
	}

	switch m.page {
	case pageStatus:
		addHint("r", "Atualizar")
		if m.currentConn.SSID != "" {
			addHint("d", "Desconectar")
		}
	case pageWifi:
		switch m.wifi.sub {
		case wifiViewList:
			addHint("↑/↓", "Navegar")
			addHint("Enter", "Conectar")
			addHint("r", "Escanear")
			addHint("/", "Filtrar")
		case wifiViewPassword:
			addHint("Enter", "Confirmar")
			addHint("Esc", "Cancelar")
		case wifiViewConnecting:
			addHint("Esc", "Cancelar")
		case wifiViewResult:
			addHint("Enter/Esc", "Voltar")
		}
	case pageDiag:
		addHint("r", "Reexecutar")
		addHint("PgUp/PgDn", "Rolar")
	}
	addHint("1-3/Tab", "Trocar aba")
	addHint("q", "Sair")

	return strings.Join(parts, helpDescStyle.Render("  •  "))
}
