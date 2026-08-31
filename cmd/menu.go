package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
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
			BorderForeground(catMauve).
			Padding(0, 1).
			MarginTop(1)

	outputTitleStyle = lipgloss.NewStyle().
				Foreground(catMauve).
				Bold(true)

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

type model struct {
	list   list.Model
	output string
	width  int
	height int
}

var menuCmd = &cobra.Command{
	Use:   "menu",
	Short: "Painel interativo profissional do NetWatch",
	Run: func(cmd *cobra.Command, args []string) {
		items := []list.Item{
			item{title: "Status da Conexão", desc: "Exibe interface, IP e detalhes do Wi-Fi atual", action: "wifi", icon: "󰖩"},
			item{title: "Escanear Redes Wi-Fi", desc: "Varre SSIDs disponíveis no alcance com nível de sinal", action: "wifi list", icon: "󰤨"},
			item{title: "Desconectar da Rede", desc: "Encerra a associação com o ponto de acesso", action: "wifi disconnect", icon: "󰌘"},
			item{title: "Diagnóstico Completo", desc: "Executa testes de ping, gateway, DNS e rotas", action: "check", icon: "󱛂"},
		}

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

		l := list.New(items, d, 0, 0)
		l.Title = "NETWATCH TUI"
		l.Styles.Title = lipgloss.NewStyle().
			Foreground(catBlue).
			Bold(true).
			MarginBottom(1)

		l.SetShowStatusBar(false)
		l.SetFilteringEnabled(true)

		m := model{list: l}

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
			i, ok := m.list.SelectedItem().(item)
			if ok {
				m.output = executeCLI(strings.Split(i.action, " ")...)
			}
			return m, nil
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.list.SetSize(msg.Width-6, msg.Height-10)
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
	s.WriteString(panelStyle.Render(m.list.View()))
	s.WriteString("\n")

	// Caixa de Output (caso um comando seja executado)
	if m.output != "" {
		var outBuilder strings.Builder
		outBuilder.WriteString(outputTitleStyle.Render(" RESULTADO DA EXECUÇÃO "))
		outBuilder.WriteString("\n\n")
		outBuilder.WriteString(m.output)

		s.WriteString(outputBoxStyle.Render(outBuilder.String()))
		s.WriteString("\n")
	}

	// Rodapé de Ajuda Estilizado
	s.WriteString("\n")
	s.WriteString(helpKeyStyle.Render("j/k/↑/↓"))
	s.WriteString(helpDescStyle.Render(" Navegar  •  "))
	s.WriteString(helpKeyStyle.Render("Enter"))
	s.WriteString(helpDescStyle.Render(" Selecionar  •  "))
	s.WriteString(helpKeyStyle.Render("/"))
	s.WriteString(helpDescStyle.Render(" Filtrar  •  "))
	s.WriteString(helpKeyStyle.Render("q"))
	s.WriteString(helpDescStyle.Render(" Sair"))

	return s.String()
}

func executeCLI(args ...string) string {
	cmd := exec.Command("netwatch", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Sprintf("Erro ao executar:\n%s", string(out))
	}
	return string(out)
}