package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

// selfPath é o caminho absoluto do binário netwatch atualmente em execução, resolvido uma
// única vez em init(). É usado para reexecutar o próprio binário a partir do menu, em vez de
// depender de um "netwatch" já instalado em $PATH (que pode divergir da versão em execução).
var selfPath = "netwatch"

func init() {
	if p, err := os.Executable(); err == nil {
		selfPath = p
	}
}

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

	outputBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			Padding(0, 1).
			MarginTop(1)

	outputTitleStyle = lipgloss.NewStyle().
				Foreground(catMauve).
				Bold(true)

	outputErrTitleStyle = lipgloss.NewStyle().
				Foreground(catRed).
				Bold(true)

	spinnerStyle = lipgloss.NewStyle().
			Foreground(catYellow)

	helpKeyStyle = lipgloss.NewStyle().
			Foreground(catBlue).
			Bold(true)

	helpDescStyle = lipgloss.NewStyle().
			Foreground(catOverlay0)
)

type item struct {
	title, desc, action, icon string
}

func (i item) Title() string       { return i.icon + " " + i.title }
func (i item) Description() string { return i.desc }
func (i item) FilterValue() string { return i.title }

// menuItems são as ações disponíveis no painel — mantidas como variável de pacote para que
// o cálculo de altura compacta da lista (compactListHeight) e a construção do model
// concordem sobre a quantidade de itens.
var menuItems = []list.Item{
	item{title: "Status da Conexão", desc: "Exibe interface, IP e detalhes do Wi-Fi atual", action: "wifi", icon: "󰖩"},
	item{title: "Escanear Redes Wi-Fi", desc: "Varre SSIDs disponíveis no alcance com nível de sinal", action: "wifi list", icon: "󰤨"},
	item{title: "Desconectar da Rede", desc: "Encerra a associação com o ponto de acesso", action: "wifi disconnect", icon: "󰌘"},
	item{title: "Diagnóstico Completo", desc: "Executa testes de ping, gateway, DNS e rotas", action: "check", icon: "󱛂"},
}

type model struct {
	list       list.Model
	viewport   viewport.Model
	spinner    spinner.Model
	output     string
	outputErr  bool
	running    bool
	actionName string
	width      int
	height     int
}

// cliResultMsg carrega o resultado (já concluído) da execução assíncrona de um subcomando.
type cliResultMsg struct {
	output string
	failed bool
}

// runCLI executa args via executeCLI em background (fora da goroutine principal do Bubble
// Tea), permitindo que a TUI continue respondendo e animando o spinner enquanto o
// subprocesso roda, em vez de travar a interface inteira até ele terminar.
func runCLI(args ...string) tea.Cmd {
	return func() tea.Msg {
		out, err := executeCLIOutput(args...)
		return cliResultMsg{output: out, failed: err != nil}
	}
}

// newMenuList constrói a lista de opções do menu principal, isolada de menuCmd.Run para
// poder ser reaproveitada na construção de um model em testes, sem depender de um tea.Program.
func newMenuList() list.Model {
	// Configuração do Delegate Visual (Estilo gonmtui/lazygit)
	d := list.NewDefaultDelegate()

	// Item Selecionado: Linha lateral em destaque + texto em Verde Catppuccin
	d.Styles.SelectedTitle = lipgloss.NewStyle().
		Foreground(catGreen).
		Bold(true).
		Border(lipgloss.NormalBorder(), false, false, false, true).
		BorderForeground(catGreen).
		PaddingLeft(1)

	d.Styles.SelectedDesc = lipgloss.NewStyle().
		Foreground(catText).
		PaddingLeft(2)

	// Item Normal
	d.Styles.NormalTitle = lipgloss.NewStyle().
		Foreground(catText).
		PaddingLeft(2)

	d.Styles.NormalDesc = lipgloss.NewStyle().
		Foreground(catOverlay0).
		PaddingLeft(2)

	l := list.New(menuItems, d, 0, 0)
	l.Title = "NETWATCH TUI"
	l.Styles.Title = lipgloss.NewStyle().
		Foreground(catBlue).
		Bold(true).
		MarginBottom(1)

	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(true)
	// O rodapé de ajuda e a paginação nativos do componente duplicavam nosso próprio rodapé
	// customizado (renderizado em View()) — desligados para eliminar a redundância visual.
	l.SetShowHelp(false)
	l.SetShowPagination(false)

	return l
}

// newSpinner constrói o spinner exibido enquanto um subcomando roda em background.
func newSpinner() spinner.Model {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = spinnerStyle
	return s
}

// compactListHeight calcula a altura mínima necessária para renderizar o título da lista e
// todos os itens, sem sobrar espaço vazio — o comportamento anterior fixava uma altura quase
// igual à do terminal inteiro mesmo havendo só 4 itens, deixando a maior parte do painel em
// branco. Respeita um teto baseado na altura real do terminal para telas muito pequenas.
func compactListHeight(itemCount, termHeight int) int {
	// título da lista + margem (3 linhas) + cada item (título, descrição, espaçamento = 3 linhas).
	// Medido empiricamente a partir de list.Model.View() — o delegate padrão do bubbles/list
	// não expõe esses números diretamente.
	needed := 3 + itemCount*3
	maxAllowed := termHeight - 12 // reserva espaço para cabeçalho, viewport de saída e rodapé
	if maxAllowed < 3 {
		maxAllowed = 3
	}
	if needed > maxAllowed {
		return maxAllowed
	}
	return needed
}

var menuCmd = &cobra.Command{
	Use:   "menu",
	Short: "Painel interativo profissional do NetWatch",
	Run: func(cmd *cobra.Command, args []string) {
		m := model{list: newMenuList(), spinner: newSpinner(), viewport: viewport.New(0, 0)}

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
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit

		case "enter":
			if m.running {
				// Já há um comando em execução — ignora novas seleções até ele terminar.
				return m, nil
			}
			i, ok := m.list.SelectedItem().(item)
			if !ok {
				return m, nil
			}
			m.running = true
			m.actionName = i.title
			m.output = ""
			m.outputErr = false
			m.viewport.SetContent("")
			return m, tea.Batch(spinner.Tick, runCLI(strings.Split(i.action, " ")...))

		case "pgup", "pgdown":
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(msg)
			return m, cmd
		}

	case spinner.TickMsg:
		if !m.running {
			return m, nil
		}
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case cliResultMsg:
		m.running = false
		m.output = msg.output
		m.outputErr = msg.failed
		m.viewport.SetContent(m.output)
		m.viewport.GotoTop()
		return m, nil

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

		listHeight := compactListHeight(len(menuItems), msg.Height)
		m.list.SetSize(msg.Width-6, listHeight)

		vpHeight := msg.Height - listHeight - 11
		if vpHeight < 3 {
			vpHeight = 3
		}
		m.viewport.Width = msg.Width - 8
		m.viewport.Height = vpHeight
		m.viewport.SetContent(m.output)
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m model) View() string {
	var s strings.Builder

	// Cabeçalho Principal
	s.WriteString(badgeStyle.Render(" NETWATCH "))
	s.WriteString(subHeaderStyle.Render("Network Manager Console"))
	s.WriteString("\n\n")

	// Painel Central com a Lista
	panel := panelStyle
	if m.width > 0 {
		panel = panel.Width(m.width - 4)
	}
	s.WriteString(panel.Render(m.list.View()))
	s.WriteString("\n")

	// Caixa de Output: estado de execução (spinner) ou resultado do último comando
	outputBox := outputBoxStyle
	if m.width > 0 {
		outputBox = outputBox.Width(m.width - 4)
	}

	switch {
	case m.running:
		outputBox = outputBox.BorderForeground(catYellow)
		var body strings.Builder
		body.WriteString(m.spinner.View())
		body.WriteString(" Executando ")
		body.WriteString(outputTitleStyle.Render(m.actionName))
		body.WriteString("...")
		s.WriteString(outputBox.Render(body.String()))
		s.WriteString("\n")

	case m.output != "":
		var title string
		if m.outputErr {
			outputBox = outputBox.BorderForeground(catRed)
			title = outputErrTitleStyle.Render(" ERRO NA EXECUÇÃO ")
		} else {
			outputBox = outputBox.BorderForeground(catMauve)
			title = outputTitleStyle.Render(" RESULTADO DA EXECUÇÃO ")
		}
		var outBuilder strings.Builder
		outBuilder.WriteString(title)
		outBuilder.WriteString("\n\n")
		outBuilder.WriteString(m.viewport.View())
		s.WriteString(outputBox.Render(outBuilder.String()))
		s.WriteString("\n")
	}

	// Rodapé de Ajuda Estilizado
	s.WriteString("\n")
	s.WriteString(helpKeyStyle.Render("j/k/↑/↓"))
	s.WriteString(helpDescStyle.Render(" Navegar  •  "))
	s.WriteString(helpKeyStyle.Render("Enter"))
	s.WriteString(helpDescStyle.Render(" Selecionar  •  "))
	if m.output != "" && !m.running {
		s.WriteString(helpKeyStyle.Render("PgUp/PgDn"))
		s.WriteString(helpDescStyle.Render(" Rolar saída  •  "))
	}
	s.WriteString(helpKeyStyle.Render("/"))
	s.WriteString(helpDescStyle.Render(" Filtrar  •  "))
	s.WriteString(helpKeyStyle.Render("q"))
	s.WriteString(helpDescStyle.Render(" Sair"))

	return s.String()
}

// cliTimeout limita quanto tempo executeCLI espera pelo subprocesso — 20s cobre com folga
// o maior timeout interno dos subcomandos (15s do "wifi connect"), evitando que a TUI trave
// indefinidamente caso o D-Bus não responda. Variável de pacote para permitir um valor menor
// em testes que verificam o cancelamento por timeout sem esperar 20s de verdade.
var cliTimeout = 20 * time.Second

// executeCLIOutput executa args reexecutando o próprio binário e retorna a saída combinada
// junto com o erro real do processo (se houver), para que o chamador possa distinguir
// sucesso de falha sem depender de heurísticas sobre o texto retornado.
func executeCLIOutput(args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), cliTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, selfPath, args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// executeCLI mantém a assinatura antiga (usada por testes existentes) como um wrapper fino
// sobre executeCLIOutput, retornando apenas a saída formatada.
func executeCLI(args ...string) string {
	out, err := executeCLIOutput(args...)
	if err != nil {
		return fmt.Sprintf("Erro ao executar:\n%s", out)
	}
	return out
}
