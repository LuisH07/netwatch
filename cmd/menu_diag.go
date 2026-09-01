package cmd

import (
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

// diagPageModel é a página "Diagnóstico": roda o mesmo cálculo usado por `netwatch check` e
// renderiza o mesmo relatório colorizado, num viewport rolável.
type diagPageModel struct {
	loaded   bool
	running  bool
	viewport viewport.Model
	lastOut  string
}

// diagRanMsg carrega o resultado (já calculado) de uma execução do diagnóstico.
type diagRanMsg struct {
	report checkReport
}

func runDiagCmd() tea.Cmd {
	return func() tea.Msg {
		return diagRanMsg{report: runDiagnostics()}
	}
}

func newDiagPageModel() diagPageModel {
	return diagPageModel{viewport: viewport.New(0, 0)}
}

func (m diagPageModel) resize(width, height int) diagPageModel {
	m.viewport.Width = width
	m.viewport.Height = height
	m.viewport.SetContent(m.lastOut)
	return m
}

// onEnter dispara a primeira execução do diagnóstico ao visitar a página, uma única vez.
// Não depende do wifi.Manager — funciona mesmo se a inicialização do Wi-Fi tiver falhado.
func (m diagPageModel) onEnter() (diagPageModel, tea.Cmd) {
	if m.loaded {
		return m, nil
	}
	m.loaded = true
	m.running = true
	return m, runDiagCmd()
}

func (m diagPageModel) Update(msg tea.Msg) (diagPageModel, tea.Cmd) {
	switch msg := msg.(type) {
	case diagRanMsg:
		m.running = false
		m.lastOut = renderCheckReport(msg.report)
		m.viewport.SetContent(m.lastOut)
		m.viewport.GotoTop()
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "r":
			if m.running {
				return m, nil
			}
			m.running = true
			return m, runDiagCmd()
		case "pgup", "pgdown":
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(msg)
			return m, cmd
		}
	}
	return m, nil
}

func (m diagPageModel) View(spinnerView string) string {
	if m.running && m.lastOut == "" {
		return spinnerView + " Executando diagnóstico..."
	}
	var b strings.Builder
	if m.running {
		b.WriteString(spinnerView)
		b.WriteString(" Reexecutando...\n\n")
	}
	b.WriteString(m.viewport.View())
	return b.String()
}
