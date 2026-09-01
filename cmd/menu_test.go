package cmd

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

func withSelfPath(t *testing.T, path string) {
	t.Helper()
	orig := selfPath
	selfPath = path
	t.Cleanup(func() { selfPath = orig })
}

func withCLITimeout(t *testing.T, d time.Duration) {
	t.Helper()
	orig := cliTimeout
	cliTimeout = d
	t.Cleanup(func() { cliTimeout = orig })
}

// buildModel replica a construção de model feita em menuCmd.Run, sem iniciar um tea.Program.
func buildModel() model {
	return model{list: newMenuList(), spinner: newSpinner(), viewport: viewport.New(0, 0)}
}

// runBatch invoca cada tea.Cmd de um tea.BatchMsg e retorna as mensagens produzidas, na
// mesma ordem — usado para testar comandos disparados via tea.Batch sem depender do runtime
// completo do Bubble Tea.
func runBatch(t *testing.T, cmd tea.Cmd) []tea.Msg {
	t.Helper()
	batch, ok := cmd().(tea.BatchMsg)
	if !ok {
		t.Fatalf("expected tea.BatchMsg, got %T", cmd())
	}
	msgs := make([]tea.Msg, 0, len(batch))
	for _, c := range batch {
		msgs = append(msgs, c())
	}
	return msgs
}

// findCLIResult procura uma cliResultMsg dentre as mensagens produzidas por runBatch.
func findCLIResult(t *testing.T, msgs []tea.Msg) cliResultMsg {
	t.Helper()
	for _, msg := range msgs {
		if res, ok := msg.(cliResultMsg); ok {
			return res
		}
	}
	t.Fatal("expected one of the batched commands to produce a cliResultMsg")
	return cliResultMsg{}
}

func TestItem_Accessors(t *testing.T) {
	i := item{title: "Título", desc: "Descrição", action: "check", icon: "X"}
	if got := i.Title(); got != "X Título" {
		t.Errorf("Title() = %q, want %q", got, "X Título")
	}
	if got := i.Description(); got != "Descrição" {
		t.Errorf("Description() = %q, want %q", got, "Descrição")
	}
	if got := i.FilterValue(); got != "Título" {
		t.Errorf("FilterValue() = %q, want %q", got, "Título")
	}
}

func TestModel_Init(t *testing.T) {
	m := buildModel()
	if cmd := m.Init(); cmd != nil {
		t.Errorf("Init() = %v, want nil", cmd)
	}
}

func TestModel_Update_WindowSize(t *testing.T) {
	m := buildModel()

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	mm, ok := updated.(model)
	if !ok {
		t.Fatalf("expected model, got %T", updated)
	}
	if mm.width != 100 || mm.height != 40 {
		t.Errorf("width/height = %d/%d, want 100/40", mm.width, mm.height)
	}
}

func TestModel_Update_QuitKeys(t *testing.T) {
	for _, key := range []string{"q", "ctrl+c"} {
		t.Run(key, func(t *testing.T) {
			m := buildModel()
			var msg tea.KeyMsg
			if key == "ctrl+c" {
				msg = tea.KeyMsg(tea.Key{Type: tea.KeyCtrlC})
			} else {
				msg = tea.KeyMsg(tea.Key{Type: tea.KeyRunes, Runes: []rune("q")})
			}

			_, cmd := m.Update(msg)
			if cmd == nil {
				t.Fatal("expected a non-nil tea.Cmd for a quit key")
			}
			if _, isQuit := cmd().(tea.QuitMsg); !isQuit {
				t.Errorf("expected cmd() to produce tea.QuitMsg, got %T", cmd())
			}
		})
	}
}

func TestModel_Update_EnterStartsAsyncExecution(t *testing.T) {
	withSelfPath(t, "/bin/echo")

	m := buildModel()
	updated, cmd := m.Update(tea.KeyMsg(tea.Key{Type: tea.KeyEnter}))
	mm, ok := updated.(model)
	if !ok {
		t.Fatalf("expected model, got %T", updated)
	}
	if !mm.running {
		t.Error("expected model.running = true immediately after Enter, before the subprocess finishes")
	}
	if mm.actionName == "" {
		t.Error("expected actionName to be set to the selected item's title")
	}
	if cmd == nil {
		t.Fatal("expected a non-nil tea.Cmd to kick off spinner ticking and the async command")
	}
}

func TestModel_Update_EnterIgnoredWhileRunning(t *testing.T) {
	m := buildModel()
	m.running = true

	_, cmd := m.Update(tea.KeyMsg(tea.Key{Type: tea.KeyEnter}))
	if cmd != nil {
		t.Error("expected Enter to be a no-op while a command is already running")
	}
}

func TestModel_Update_CLIResultEndsRunningState(t *testing.T) {
	withSelfPath(t, "/bin/echo")

	m := buildModel()
	updated, cmd := m.Update(tea.KeyMsg(tea.Key{Type: tea.KeyEnter}))
	mm := updated.(model)

	result := findCLIResult(t, runBatch(t, cmd))

	updated2, _ := mm.Update(result)
	mm2, ok := updated2.(model)
	if !ok {
		t.Fatalf("expected model, got %T", updated2)
	}
	if mm2.running {
		t.Error("expected running = false after receiving cliResultMsg")
	}
	if mm2.outputErr {
		t.Error("expected outputErr = false for a successful command")
	}
	if !strings.Contains(mm2.output, "wifi") {
		t.Errorf("expected output to contain the executed action's args, got %q", mm2.output)
	}
}

func TestModel_Update_CLIResultMarksFailure(t *testing.T) {
	withSelfPath(t, "/bin/false")

	m := buildModel()
	updated, cmd := m.Update(tea.KeyMsg(tea.Key{Type: tea.KeyEnter}))
	mm := updated.(model)

	result := findCLIResult(t, runBatch(t, cmd))

	updated2, _ := mm.Update(result)
	mm2 := updated2.(model)
	if !mm2.outputErr {
		t.Error("expected outputErr = true when the executed subcommand fails")
	}
}

func TestModel_Update_SpinnerTickIgnoredWhenNotRunning(t *testing.T) {
	m := buildModel()
	m.running = false

	_, cmd := m.Update(spinner.TickMsg{})
	if cmd != nil {
		t.Error("expected spinner ticks to be ignored when no command is running")
	}
}

func TestModel_Update_PageScrollForwardedToViewport(t *testing.T) {
	m := buildModel()
	m.output = strings.Repeat("linha\n", 200)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	mm := updated.(model)

	updated2, _ := mm.Update(tea.KeyMsg(tea.Key{Type: tea.KeyPgDown}))
	mm2 := updated2.(model)
	if mm2.viewport.YOffset == 0 {
		t.Error("expected pgdown to move the viewport's scroll offset")
	}
}

func TestCompactListHeight(t *testing.T) {
	tests := []struct {
		name       string
		itemCount  int
		termHeight int
		want       int
	}{
		{"plenty of room", 4, 100, 3 + 4*3},
		{"clamped by small terminal", 4, 20, 8},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := compactListHeight(tt.itemCount, tt.termHeight); got != tt.want {
				t.Errorf("compactListHeight(%d, %d) = %d, want %d", tt.itemCount, tt.termHeight, got, tt.want)
			}
		})
	}
}

func TestModel_View_NoPanicAndContainsHeader(t *testing.T) {
	m := buildModel()
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	mm := updated.(model)
	mm.output = "algum resultado"

	view := mm.View()
	if !strings.Contains(view, "NETWATCH") {
		t.Error("expected view to contain the NETWATCH header")
	}
	if !strings.Contains(view, "RESULTADO DA EXECUÇÃO") {
		t.Error("expected view to render the output box when output is set")
	}
}

func TestExecuteCLI_Success(t *testing.T) {
	withSelfPath(t, "/bin/echo")

	out := executeCLI("hello")
	if strings.TrimSpace(out) != "hello" {
		t.Errorf("executeCLI output = %q, want \"hello\"", out)
	}
}

func TestExecuteCLI_CommandError(t *testing.T) {
	withSelfPath(t, "/bin/false")

	out := executeCLI()
	if !strings.Contains(out, "Erro ao executar") {
		t.Errorf("expected error message for a failing command, got %q", out)
	}
}

func TestExecuteCLI_Timeout(t *testing.T) {
	withSelfPath(t, "/bin/sleep")
	withCLITimeout(t, 50*time.Millisecond)

	start := time.Now()
	out := executeCLI("5")
	elapsed := time.Since(start)

	if elapsed > 3*time.Second {
		t.Errorf("executeCLI took %v, expected it to be killed near the 50ms timeout", elapsed)
	}
	if !strings.Contains(out, "Erro ao executar") {
		t.Errorf("expected error message when the subprocess is killed by timeout, got %q", out)
	}
}
