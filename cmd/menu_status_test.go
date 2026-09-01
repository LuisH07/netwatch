package cmd

import (
	"errors"
	"strings"
	"testing"

	"netwatch/internal/wifi"
)

func TestStatusPage_RefreshKey(t *testing.T) {
	m := newStatusPageModel()
	fm := &fakeManager{currentResult: wifi.Connection{SSID: "RedeAtual"}}

	_, cmd := m.Update(keyRune('r'), fm, wifi.Connection{})
	if cmd == nil {
		t.Fatal("expected 'r' to trigger a refresh command")
	}
	msg, ok := cmd().(currentConnMsg)
	if !ok {
		t.Fatalf("expected currentConnMsg, got %T", cmd())
	}
	if msg.conn.SSID != "RedeAtual" {
		t.Errorf("SSID = %q, want RedeAtual", msg.conn.SSID)
	}
}

func TestStatusPage_RefreshNoopWithoutManager(t *testing.T) {
	m := newStatusPageModel()
	_, cmd := m.Update(keyRune('r'), nil, wifi.Connection{})
	if cmd != nil {
		t.Error("expected no command when mgr is nil")
	}
}

func TestStatusPage_DisconnectKey(t *testing.T) {
	m := newStatusPageModel()
	fm := &fakeManager{}

	updated, cmd := m.Update(keyRune('d'), fm, wifi.Connection{SSID: "RedeAtual"})
	if !updated.disconnecting {
		t.Error("expected disconnecting = true immediately")
	}
	if cmd == nil {
		t.Fatal("expected a disconnect command")
	}
	msg, ok := cmd().(statusDisconnectDoneMsg)
	if !ok {
		t.Fatalf("expected statusDisconnectDoneMsg, got %T", cmd())
	}
	if msg.err != nil {
		t.Errorf("expected nil error, got %v", msg.err)
	}
}

func TestStatusPage_DisconnectNoopWithoutActiveConnection(t *testing.T) {
	m := newStatusPageModel()
	fm := &fakeManager{}

	_, cmd := m.Update(keyRune('d'), fm, wifi.Connection{})
	if cmd != nil {
		t.Error("expected no command when there is no active connection to disconnect from")
	}
}

func TestStatusPage_DisconnectNoopWhileAlreadyDisconnecting(t *testing.T) {
	m := newStatusPageModel()
	m.disconnecting = true
	fm := &fakeManager{}

	_, cmd := m.Update(keyRune('d'), fm, wifi.Connection{SSID: "RedeAtual"})
	if cmd != nil {
		t.Error("expected no command while a disconnect is already in flight")
	}
}

func TestStatusPage_HandleDisconnectDone(t *testing.T) {
	m := newStatusPageModel()
	m.disconnecting = true

	updated, _ := m.handleDisconnectDone(statusDisconnectDoneMsg{err: errors.New("boom")})
	if updated.disconnecting {
		t.Error("expected disconnecting = false after handleDisconnectDone")
	}
	if updated.disconnectErr == nil {
		t.Error("expected disconnectErr to be set")
	}
}

func TestStatusPage_View_ManagerError(t *testing.T) {
	m := newStatusPageModel()
	view := m.View(wifi.Connection{}, nil, errors.New("dbus down"), "")
	if !strings.Contains(view, "dbus down") {
		t.Errorf("expected the manager error to be rendered, got: %q", view)
	}
}

func TestStatusPage_View_NoActiveConnection(t *testing.T) {
	m := newStatusPageModel()
	view := m.View(wifi.Connection{}, wifi.ErrNoActiveConnection, nil, "")
	if !strings.Contains(view, "Nenhuma rede") {
		t.Errorf("expected the no-active-connection message, got: %q", view)
	}
}

func TestStatusPage_View_Connected(t *testing.T) {
	m := newStatusPageModel()
	view := m.View(wifi.Connection{SSID: "RedeAtual", Interface: "wlan0"}, nil, nil, "")
	if !strings.Contains(view, "RedeAtual") || !strings.Contains(view, "wlan0") {
		t.Errorf("expected connection details in view, got: %q", view)
	}
}

func TestStatusPage_View_Disconnecting(t *testing.T) {
	m := newStatusPageModel()
	m.disconnecting = true
	view := m.View(wifi.Connection{SSID: "RedeAtual"}, nil, nil, "<spinner>")
	if !strings.Contains(view, "Desconectando") {
		t.Errorf("expected a disconnecting indicator, got: %q", view)
	}
}
