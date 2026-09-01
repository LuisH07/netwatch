package cmd

import (
	"errors"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"netwatch/internal/wifi"
)

// buildModel replica a construção de model feita em menuCmd.Run, sem iniciar um tea.Program.
func buildModel() model {
	return model{
		spinner: newSpinner(),
		status:  newStatusPageModel(),
		wifi:    newWifiPageModel(),
		diag:    newDiagPageModel(),
	}
}

func keyRune(r rune) tea.KeyMsg { return tea.KeyMsg(tea.Key{Type: tea.KeyRunes, Runes: []rune{r}}) }

func TestModel_Init(t *testing.T) {
	m := buildModel()
	if cmd := m.Init(); cmd == nil {
		t.Error("expected Init() to kick off manager initialization")
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
				msg = keyRune('q')
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

func TestModel_Update_WindowSize(t *testing.T) {
	m := buildModel()
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	mm := updated.(model)
	if mm.width != 100 || mm.height != 40 {
		t.Errorf("width/height = %d/%d, want 100/40", mm.width, mm.height)
	}
	if mm.wifi.list.Width() == 0 {
		t.Error("expected the Wi-Fi list to be resized on WindowSizeMsg regardless of active page")
	}
}

func TestModel_Update_TabCyclesPages(t *testing.T) {
	m := buildModel()
	if m.page != pageStatus {
		t.Fatalf("expected initial page to be pageStatus, got %v", m.page)
	}

	updated, _ := m.Update(tea.KeyMsg(tea.Key{Type: tea.KeyTab}))
	mm := updated.(model)
	if mm.page != pageWifi {
		t.Errorf("page after Tab = %v, want pageWifi", mm.page)
	}

	updated, _ = mm.Update(tea.KeyMsg(tea.Key{Type: tea.KeyShiftTab}))
	mm = updated.(model)
	if mm.page != pageStatus {
		t.Errorf("page after Shift+Tab = %v, want pageStatus", mm.page)
	}
}

func TestModel_Update_NumberKeysJumpToPage(t *testing.T) {
	m := buildModel()
	updated, _ := m.Update(keyRune('3'))
	mm := updated.(model)
	if mm.page != pageDiag {
		t.Errorf("page after '3' = %v, want pageDiag", mm.page)
	}
}

func TestModel_Update_TabTriggersOnEnterForWifi(t *testing.T) {
	m := buildModel()
	m.mgr = &fakeManager{}

	updated, cmd := m.Update(keyRune('2'))
	mm := updated.(model)
	if mm.page != pageWifi {
		t.Fatalf("expected pageWifi, got %v", mm.page)
	}
	if !mm.wifi.scanning {
		t.Error("expected entering the Wi-Fi page to trigger an automatic scan")
	}
	if cmd == nil {
		t.Fatal("expected a non-nil scan command")
	}
}

// TestModel_Update_GlobalKeysGatedDuringPasswordEntry é uma regressão: sem o gate de
// capturingInput, digitar dígitos ou "q" numa senha trocaria de aba/encerraria o programa em
// vez de compor o texto digitado.
func TestModel_Update_GlobalKeysGatedDuringPasswordEntry(t *testing.T) {
	m := buildModel()
	m.page = pageWifi
	m.wifi.sub = wifiViewPassword
	m.wifi.pwInput.Focus()

	updated, cmd := m.Update(keyRune('q'))
	mm := updated.(model)
	if mm.page != pageWifi {
		t.Error("expected 'q' to be forwarded to the password field, not trigger a page switch")
	}
	if cmd != nil {
		if _, isQuit := cmd().(tea.QuitMsg); isQuit {
			t.Error("expected 'q' during password entry to NOT quit the program")
		}
	}
	if mm.wifi.pwInput.Value() != "q" {
		t.Errorf("expected 'q' to be typed into the password field, got value %q", mm.wifi.pwInput.Value())
	}
}

// TestModel_Update_GlobalKeysGatedWhileFilteringWifiList é a mesma regressão para o modo de
// filtro da lista de Wi-Fi.
func TestModel_Update_GlobalKeysGatedWhileFilteringWifiList(t *testing.T) {
	m := buildModel()
	m.page = pageWifi
	m.wifi.list.SetItems(apItemsFromAPs([]wifi.AccessPoint{{SSID: "Rede1"}, {SSID: "Rede2"}}, ""))
	// Entra em modo de filtro (tecla "/").
	m.wifi.list, _ = m.wifi.list.Update(keyRune('/'))
	if m.wifi.list.FilterState() != list.Filtering {
		t.Fatal("setup failed: expected the list to be in filtering state")
	}

	updated, _ := m.Update(keyRune('2'))
	mm := updated.(model)
	if mm.page != pageWifi {
		t.Error("expected '2' to be forwarded to the filter input, not trigger a page jump")
	}
}

func TestModel_Update_BackgroundWifiResultAppliedAfterPageSwitch(t *testing.T) {
	m := buildModel()
	m.mgr = &fakeManager{}
	m.page = pageDiag // usuário já saiu da aba Wi-Fi

	updated, _ := m.Update(wifiScannedMsg{aps: []wifi.AccessPoint{{SSID: "RedeX"}}})
	mm := updated.(model)
	if mm.page != pageDiag {
		t.Fatal("page switch should not happen implicitly")
	}
	if len(mm.wifi.list.Items()) != 1 {
		t.Errorf("expected the scan result to be applied to the Wi-Fi page even while on a different tab, got %d items", len(mm.wifi.list.Items()))
	}
}

func TestModel_Update_ManagerReadyFetchesCurrentConn(t *testing.T) {
	m := buildModel()
	fm := &fakeManager{currentResult: wifi.Connection{SSID: "RedeAtual"}}

	updated, cmd := m.Update(managerReadyMsg{mgr: fm})
	mm := updated.(model)
	if mm.mgr != fm {
		t.Fatal("expected mgr to be stored")
	}
	if cmd == nil {
		t.Fatal("expected a command to fetch the current connection")
	}

	connMsg, ok := findMsgOfType[currentConnMsg](t, cmd())
	if !ok {
		t.Fatal("expected a currentConnMsg among the produced commands")
	}
	if connMsg.conn.SSID != "RedeAtual" {
		t.Errorf("SSID = %q, want RedeAtual", connMsg.conn.SSID)
	}
}

// findMsgOfType invoca msg (desempacotando um tea.BatchMsg se necessário) e procura uma
// mensagem do tipo T entre o(s) resultado(s).
func findMsgOfType[T any](t *testing.T, msg tea.Msg) (T, bool) {
	t.Helper()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			if v, ok := findMsgOfType[T](t, c()); ok {
				return v, true
			}
		}
		var zero T
		return zero, false
	}
	v, ok := msg.(T)
	return v, ok
}

func TestModel_Update_ManagerReadyError_NoCurrentConnFetch(t *testing.T) {
	m := buildModel()
	updated, cmd := m.Update(managerReadyMsg{err: errors.New("dbus down")})
	mm := updated.(model)
	if mm.mgrErr == nil {
		t.Fatal("expected mgrErr to be set")
	}
	if cmd != nil {
		t.Error("expected no follow-up command when manager init fails")
	}
}

// TestModel_Update_EnteringStatusPageRefetchesCurrentConn é uma regressão: sem reconsultar
// o NetworkManager a cada entrada na página Status, uma rede que caiu por qualquer motivo
// externo à TUI (sinal, roteador, etc.) continuava aparecendo como conectada indefinidamente,
// já que currentConn só era buscado uma vez no início.
func TestModel_Update_EnteringStatusPageRefetchesCurrentConn(t *testing.T) {
	fm := &fakeManager{currentResult: wifi.Connection{SSID: "RedeAtual"}}

	for name, transition := range map[string]func(m model) (tea.Model, tea.Cmd){
		"tab":    func(m model) (tea.Model, tea.Cmd) { return m.Update(tea.KeyMsg(tea.Key{Type: tea.KeyTab})) },
		"number": func(m model) (tea.Model, tea.Cmd) { return m.Update(keyRune('1')) },
	} {
		t.Run(name, func(t *testing.T) {
			m := buildModel()
			m.mgr = fm
			m.page = pageDiag // começa fora de Status

			updated, cmd := transition(m)
			mm := updated.(model)
			if mm.page != pageStatus {
				t.Fatalf("expected to land on pageStatus, got %v", mm.page)
			}
			if cmd == nil {
				t.Fatal("expected a command to refetch the current connection")
			}
			if _, ok := findMsgOfType[currentConnMsg](t, cmd()); !ok {
				t.Fatal("expected a currentConnMsg among the produced commands")
			}
		})
	}
}

func TestModel_View_NoPanicAndShowsTabs(t *testing.T) {
	m := buildModel()
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	mm := updated.(model)

	view := mm.View()
	for _, want := range []string{"NETWATCH", "Status", "Wi-Fi", "Diagnóstico"} {
		if !strings.Contains(view, want) {
			t.Errorf("expected view to contain %q", want)
		}
	}
}
