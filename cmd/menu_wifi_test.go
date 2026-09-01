package cmd

import (
	"errors"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"netwatch/internal/wifi"
)

func TestWifiPage_OnEnter_TriggersScanOnce(t *testing.T) {
	m := newWifiPageModel()
	fm := &fakeManager{}

	updated, cmd := m.onEnter(fm)
	if !updated.scanning || !updated.loaded {
		t.Fatal("expected scanning=true, loaded=true after first onEnter")
	}
	if cmd == nil {
		t.Fatal("expected a scan command")
	}

	// Segunda chamada não deve re-escanear.
	updated2, cmd2 := updated.onEnter(fm)
	if cmd2 != nil {
		t.Error("expected onEnter to be a no-op after the page has already loaded")
	}
	_ = updated2
}

func TestWifiPage_OnEnter_NoopWithoutManager(t *testing.T) {
	m := newWifiPageModel()
	updated, cmd := m.onEnter(nil)
	if updated.loaded || cmd != nil {
		t.Error("expected onEnter to do nothing when mgr is nil")
	}
}

func TestWifiPage_ScannedMsg_PopulatesListWithConnectedBadge(t *testing.T) {
	m := newWifiPageModel()
	m.scanning = true

	aps := []wifi.AccessPoint{
		{SSID: "RedeAtual", Secured: true},
		{SSID: "OutraRede", Secured: false, Known: true},
	}
	updated, _ := m.Update(wifiScannedMsg{aps: aps}, nil, "RedeAtual")

	if updated.scanning {
		t.Error("expected scanning=false after wifiScannedMsg")
	}
	items := updated.list.Items()
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	first := items[0].(apItem)
	if !first.connected {
		t.Error("expected the AP matching currentSSID to be marked connected")
	}
	second := items[1].(apItem)
	if second.connected {
		t.Error("expected the other AP to not be marked connected")
	}
	if !strings.Contains(second.Title(), "Conhecida") {
		t.Errorf("expected known-network badge in title, got %q", second.Title())
	}
}

func TestWifiPage_ScannedMsg_Error(t *testing.T) {
	m := newWifiPageModel()
	updated, _ := m.Update(wifiScannedMsg{err: errors.New("scan failed")}, nil, "")
	if updated.scanErr == nil {
		t.Error("expected scanErr to be set")
	}
	if !strings.Contains(updated.View(nil, ""), "scan failed") {
		t.Error("expected the scan error to be rendered")
	}
}

func TestWifiPage_RescanKey(t *testing.T) {
	m := newWifiPageModel()
	m.loaded = true
	fm := &fakeManager{}

	updated, cmd := m.Update(keyRune('r'), fm, "")
	if !updated.scanning {
		t.Error("expected 'r' to trigger scanning")
	}
	if cmd == nil {
		t.Fatal("expected a scan command")
	}
}

func TestWifiPage_ConnectOpenNetwork(t *testing.T) {
	m := newWifiPageModel()
	m.list.SetItems(apItemsFromAPs([]wifi.AccessPoint{{SSID: "RedeAberta", Secured: false}}, ""))
	fm := &fakeManager{}

	updated, cmd := m.Update(tea.KeyMsg(tea.Key{Type: tea.KeyEnter}), fm, "")
	if updated.sub != wifiViewConnecting {
		t.Fatalf("expected sub = wifiViewConnecting, got %v", updated.sub)
	}
	if cmd == nil {
		t.Fatal("expected a connect command")
	}
	msg, ok := cmd().(wifiConnectResultMsg)
	if !ok {
		t.Fatalf("expected wifiConnectResultMsg, got %T", cmd())
	}
	if msg.ssid != "RedeAberta" || msg.err != nil {
		t.Errorf("unexpected result: %+v", msg)
	}
	if fm.connectCalledPwd != "" {
		t.Errorf("expected empty password for an open network, got %q", fm.connectCalledPwd)
	}
}

func TestWifiPage_ConnectSecuredNetwork_GoesToPasswordThenConnects(t *testing.T) {
	m := newWifiPageModel()
	m.list.SetItems(apItemsFromAPs([]wifi.AccessPoint{{SSID: "RedeSegura", Secured: true}}, ""))
	fm := &fakeManager{}

	updated, cmd := m.Update(tea.KeyMsg(tea.Key{Type: tea.KeyEnter}), fm, "")
	if updated.sub != wifiViewPassword {
		t.Fatalf("expected sub = wifiViewPassword, got %v", updated.sub)
	}
	if cmd == nil {
		t.Fatal("expected textinput.Blink command")
	}

	// Digita a senha e confirma.
	for _, r := range "senha123" {
		updated, _ = updated.Update(keyRune(r), fm, "")
	}
	updated, cmd = updated.Update(tea.KeyMsg(tea.Key{Type: tea.KeyEnter}), fm, "")
	if updated.sub != wifiViewConnecting {
		t.Fatalf("expected sub = wifiViewConnecting, got %v", updated.sub)
	}
	msg, ok := cmd().(wifiConnectResultMsg)
	if !ok {
		t.Fatalf("expected wifiConnectResultMsg, got %T", cmd())
	}
	if msg.ssid != "RedeSegura" {
		t.Errorf("ssid = %q, want RedeSegura", msg.ssid)
	}
	if fm.connectCalledPwd != "senha123" {
		t.Errorf("password forwarded = %q, want senha123", fm.connectCalledPwd)
	}
}

func TestWifiPage_ConnectSecuredNetwork_EmptyPasswordDoesNothing(t *testing.T) {
	m := newWifiPageModel()
	m.list.SetItems(apItemsFromAPs([]wifi.AccessPoint{{SSID: "RedeSegura", Secured: true}}, ""))
	fm := &fakeManager{}
	m, _ = m.Update(tea.KeyMsg(tea.Key{Type: tea.KeyEnter}), fm, "")

	updated, cmd := m.Update(tea.KeyMsg(tea.Key{Type: tea.KeyEnter}), fm, "")
	if updated.sub != wifiViewPassword {
		t.Error("expected to remain on the password view when the password is empty")
	}
	if cmd != nil {
		t.Error("expected no connect attempt with an empty password")
	}
}

func TestWifiPage_PasswordView_EscCancelsBackToList(t *testing.T) {
	m := newWifiPageModel()
	m.sub = wifiViewPassword
	m.pwInput.Focus()

	updated, _ := m.Update(tea.KeyMsg(tea.Key{Type: tea.KeyEsc}), nil, "")
	if updated.sub != wifiViewList {
		t.Errorf("expected sub = wifiViewList after Esc, got %v", updated.sub)
	}
}

func TestWifiPage_EnterOnAlreadyConnectedAPIsNoop(t *testing.T) {
	m := newWifiPageModel()
	m.list.SetItems(apItemsFromAPs([]wifi.AccessPoint{{SSID: "RedeAtual", Secured: true}}, "RedeAtual"))
	fm := &fakeManager{}

	updated, cmd := m.Update(tea.KeyMsg(tea.Key{Type: tea.KeyEnter}), fm, "RedeAtual")
	if updated.sub != wifiViewList {
		t.Error("expected selecting the already-connected AP to be a no-op")
	}
	if cmd != nil {
		t.Error("expected no command when selecting the already-connected AP")
	}
}

// TestWifiPage_ConnectingEscCancelsAndInvalidatesGen é uma regressão: cancelar uma tentativa
// de conexão em andamento deve invalidar seu resultado, para que um wifiConnectResultMsg
// tardio dessa tentativa cancelada não seja aplicado incorretamente.
func TestWifiPage_ConnectingEscCancelsAndInvalidatesGen(t *testing.T) {
	m := newWifiPageModel()
	m.list.SetItems(apItemsFromAPs([]wifi.AccessPoint{{SSID: "RedeAberta"}}, ""))
	fm := &fakeManager{}

	updated, cmd := m.Update(tea.KeyMsg(tea.Key{Type: tea.KeyEnter}), fm, "")
	if updated.sub != wifiViewConnecting {
		t.Fatal("setup failed: expected wifiViewConnecting")
	}
	staleResult := cmd() // simula o resultado da tentativa que será cancelada

	updated, cancelCmd := updated.Update(tea.KeyMsg(tea.Key{Type: tea.KeyEsc}), fm, "")
	if updated.sub != wifiViewList {
		t.Errorf("expected Esc to cancel back to wifiViewList, got %v", updated.sub)
	}
	if cancelCmd != nil {
		t.Error("expected no command from canceling")
	}

	// O resultado da tentativa cancelada, se ainda chegar, deve ser ignorado.
	finalModel, finalCmd := updated.Update(staleResult, fm, "")
	if finalModel.sub != wifiViewList {
		t.Errorf("expected the stale result to be ignored (sub should stay wifiViewList), got %v", finalModel.sub)
	}
	if finalCmd != nil {
		t.Error("expected no command from applying a stale result")
	}
}

func TestWifiPage_ResultView_DismissWithEnterRescans(t *testing.T) {
	m := newWifiPageModel()
	m.sub = wifiViewResult
	m.connectOK = true
	m.connectedSSID = "RedeAberta"
	fm := &fakeManager{}

	updated, cmd := m.Update(tea.KeyMsg(tea.Key{Type: tea.KeyEnter}), fm, "")
	if updated.sub != wifiViewList {
		t.Errorf("expected sub = wifiViewList, got %v", updated.sub)
	}
	if !updated.scanning {
		t.Error("expected dismissing the result to trigger a rescan")
	}
	if cmd == nil {
		t.Error("expected a scan command")
	}
}

func TestWifiPage_CapturingInput(t *testing.T) {
	m := newWifiPageModel()
	if m.capturingInput() {
		t.Error("expected capturingInput = false in the default list view")
	}

	m.sub = wifiViewPassword
	if !m.capturingInput() {
		t.Error("expected capturingInput = true in the password view")
	}

	m.sub = wifiViewList
	m.list.SetItems(apItemsFromAPs([]wifi.AccessPoint{{SSID: "Rede1"}}, ""))
	m.list, _ = m.list.Update(keyRune('/'))
	if m.list.FilterState() != list.Filtering {
		t.Fatal("setup failed: expected filtering state")
	}
	if !m.capturingInput() {
		t.Error("expected capturingInput = true while filtering")
	}
}

func TestWifiPage_View_ManagerError(t *testing.T) {
	m := newWifiPageModel()
	view := m.View(errors.New("dbus down"), "")
	if !strings.Contains(view, "dbus down") {
		t.Errorf("expected the manager error to be rendered, got: %q", view)
	}
}

func TestWifiPage_View_PasswordPrompt(t *testing.T) {
	m := newWifiPageModel()
	m.sub = wifiViewPassword
	m.targetSSID = "RedeSegura"

	view := m.View(nil, "")
	if !strings.Contains(view, "RedeSegura") {
		t.Errorf("expected the target SSID in the password prompt, got: %q", view)
	}
}

func TestWifiPage_View_Connecting(t *testing.T) {
	m := newWifiPageModel()
	m.sub = wifiViewConnecting
	m.targetSSID = "RedeAberta"

	view := m.View(nil, "<spinner>")
	if !strings.Contains(view, "RedeAberta") || !strings.Contains(view, "<spinner>") {
		t.Errorf("expected spinner + target SSID while connecting, got: %q", view)
	}
}

func TestWifiPage_View_ResultSuccess(t *testing.T) {
	m := newWifiPageModel()
	m.sub = wifiViewResult
	m.connectOK = true
	m.connectedSSID = "RedeAberta"

	view := m.View(nil, "")
	if !strings.Contains(view, "sucesso") || !strings.Contains(view, "RedeAberta") {
		t.Errorf("expected a success message, got: %q", view)
	}
}

func TestWifiPage_View_ResultFailure(t *testing.T) {
	m := newWifiPageModel()
	m.sub = wifiViewResult
	m.connectOK = false
	m.connectedSSID = "RedeAberta"
	m.connectErr = errors.New("senha incorreta")

	view := m.View(nil, "")
	if !strings.Contains(view, "Falha") || !strings.Contains(view, "senha incorreta") {
		t.Errorf("expected a failure message with the error, got: %q", view)
	}
}

func TestWifiPage_View_ScanningEmptyList(t *testing.T) {
	m := newWifiPageModel()
	m.scanning = true

	view := m.View(nil, "<spinner>")
	if !strings.Contains(view, "Escaneando") {
		t.Errorf("expected a full-page scanning indicator, got: %q", view)
	}
}

func TestWifiPage_View_RescanningWithExistingItems(t *testing.T) {
	m := newWifiPageModel()
	m.list.SetSize(80, 20)
	m.list.SetItems(apItemsFromAPs([]wifi.AccessPoint{{SSID: "Rede1"}}, ""))
	m.scanning = true

	view := m.View(nil, "<spinner>")
	if !strings.Contains(view, "Atualizando") {
		t.Errorf("expected an updating indicator alongside the stale list, got: %q", view)
	}
	if !strings.Contains(view, "Rede1") {
		t.Errorf("expected the stale list to still be visible while rescanning, got: %q", view)
	}
}
