package cmd

import (
	"context"
	"errors"
	"netwatch/internal/wifi"
	"testing"
)

type fakeManager struct {
	currentResult    wifi.Connection
	currentErr       error
	listResult       []wifi.AccessPoint
	listErr          error
	connectErr       error
	connectCalledPwd string
	disconnectErr    error
}

func (f *fakeManager) List(context.Context) ([]wifi.AccessPoint, error) {
	return f.listResult, f.listErr
}
func (f *fakeManager) Connect(_ context.Context, ssid, password string) error {
	f.connectCalledPwd = password
	return f.connectErr
}
func (f *fakeManager) Disconnect(context.Context) error { return f.disconnectErr }
func (f *fakeManager) Current(context.Context) (wifi.Connection, error) {
	return f.currentResult, f.currentErr
}

func withManager(t *testing.T, m wifi.Manager, mgrErr error) {
	t.Helper()
	orig := newManager
	newManager = func() (wifi.Manager, error) { return m, mgrErr }
	t.Cleanup(func() { newManager = orig })
}

func withReadPassword(t *testing.T, pw string, err error) {
	t.Helper()
	orig := readPassword
	readPassword = func() (string, error) { return pw, err }
	t.Cleanup(func() { readPassword = orig })
}

// --- wifiCmd (status) ---

func TestWifiCmd_ManagerInitError(t *testing.T) {
	withManager(t, nil, errors.New("dbus unavailable"))

	err := wifiCmd.RunE(wifiCmd, nil)
	var exitErr *exitError
	if !errors.As(err, &exitErr) || exitErr.code != 2 {
		t.Fatalf("expected exitError{code:2}, got: %v", err)
	}
}

func TestWifiCmd_NoActiveConnection(t *testing.T) {
	withManager(t, &fakeManager{currentErr: wifi.ErrNoActiveConnection}, nil)

	if err := wifiCmd.RunE(wifiCmd, nil); err != nil {
		t.Fatalf("expected nil error for ErrNoActiveConnection, got: %v", err)
	}
}

func TestWifiCmd_RealDBusError(t *testing.T) {
	withManager(t, &fakeManager{currentErr: errors.New("dbus timeout")}, nil)

	err := wifiCmd.RunE(wifiCmd, nil)
	var exitErr *exitError
	if !errors.As(err, &exitErr) || exitErr.code != 2 {
		t.Fatalf("expected exitError{code:2} for a real D-Bus error, got: %v", err)
	}
}

func TestWifiCmd_ConnectedSuccess(t *testing.T) {
	withManager(t, &fakeManager{currentResult: wifi.Connection{SSID: "MinhaRede", Interface: "wlan0"}}, nil)

	if err := wifiCmd.RunE(wifiCmd, nil); err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
}

// --- wifiListCmd ---

func TestWifiListCmd_ManagerInitError(t *testing.T) {
	withManager(t, nil, errors.New("dbus unavailable"))

	err := wifiListCmd.RunE(wifiListCmd, nil)
	var exitErr *exitError
	if !errors.As(err, &exitErr) || exitErr.code != 2 {
		t.Fatalf("expected exitError{code:2}, got: %v", err)
	}
}

func TestWifiListCmd_ListError(t *testing.T) {
	withManager(t, &fakeManager{listErr: errors.New("scan failed")}, nil)

	err := wifiListCmd.RunE(wifiListCmd, nil)
	var exitErr *exitError
	if !errors.As(err, &exitErr) || exitErr.code != 1 {
		t.Fatalf("expected exitError{code:1}, got: %v", err)
	}
}

func TestWifiListCmd_Success(t *testing.T) {
	withManager(t, &fakeManager{listResult: []wifi.AccessPoint{{SSID: "Rede1", Secured: true}}}, nil)

	if err := wifiListCmd.RunE(wifiListCmd, nil); err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
}

// --- wifiConnectCmd ---

func TestWifiConnectCmd_ManagerInitError(t *testing.T) {
	withManager(t, nil, errors.New("dbus unavailable"))

	err := wifiConnectCmd.RunE(wifiConnectCmd, []string{"MinhaRede"})
	var exitErr *exitError
	if !errors.As(err, &exitErr) || exitErr.code != 2 {
		t.Fatalf("expected exitError{code:2}, got: %v", err)
	}
}

func TestWifiConnectCmd_OpenNetwork_Success(t *testing.T) {
	mgr := &fakeManager{listResult: []wifi.AccessPoint{{SSID: "RedeAberta", Secured: false}}}
	withManager(t, mgr, nil)

	if err := wifiConnectCmd.RunE(wifiConnectCmd, []string{"RedeAberta"}); err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
	if mgr.connectCalledPwd != "" {
		t.Errorf("expected empty password for open network, got %q", mgr.connectCalledPwd)
	}
}

func TestWifiConnectCmd_SecuredNetwork_PromptsAndUsesPassword(t *testing.T) {
	mgr := &fakeManager{listResult: []wifi.AccessPoint{{SSID: "RedeSegura", Secured: true}}}
	withManager(t, mgr, nil)
	withReadPassword(t, "senha123", nil)

	if err := wifiConnectCmd.RunE(wifiConnectCmd, []string{"RedeSegura"}); err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
	if mgr.connectCalledPwd != "senha123" {
		t.Errorf("expected password to be forwarded to Connect, got %q", mgr.connectCalledPwd)
	}
}

func TestWifiConnectCmd_PasswordReadError(t *testing.T) {
	mgr := &fakeManager{listResult: []wifi.AccessPoint{{SSID: "RedeSegura", Secured: true}}}
	withManager(t, mgr, nil)
	withReadPassword(t, "", errors.New("tty error"))

	err := wifiConnectCmd.RunE(wifiConnectCmd, []string{"RedeSegura"})
	var exitErr *exitError
	if !errors.As(err, &exitErr) || exitErr.code != 1 {
		t.Fatalf("expected exitError{code:1} when password read fails, got: %v", err)
	}
}

func TestWifiConnectCmd_ConnectFails(t *testing.T) {
	mgr := &fakeManager{
		listResult: []wifi.AccessPoint{{SSID: "RedeAberta", Secured: false}},
		connectErr: errors.New("auth failed"),
	}
	withManager(t, mgr, nil)

	err := wifiConnectCmd.RunE(wifiConnectCmd, []string{"RedeAberta"})
	var exitErr *exitError
	if !errors.As(err, &exitErr) || exitErr.code != 1 {
		t.Fatalf("expected exitError{code:1}, got: %v", err)
	}
}

// --- wifiDisconnectCmd ---

func TestWifiDisconnectCmd_ManagerInitError(t *testing.T) {
	withManager(t, nil, errors.New("dbus unavailable"))

	err := wifiDisconnectCmd.RunE(wifiDisconnectCmd, nil)
	var exitErr *exitError
	if !errors.As(err, &exitErr) || exitErr.code != 2 {
		t.Fatalf("expected exitError{code:2}, got: %v", err)
	}
}

func TestWifiDisconnectCmd_Success(t *testing.T) {
	withManager(t, &fakeManager{}, nil)

	if err := wifiDisconnectCmd.RunE(wifiDisconnectCmd, nil); err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
}

func TestWifiDisconnectCmd_Fails(t *testing.T) {
	withManager(t, &fakeManager{disconnectErr: errors.New("already down")}, nil)

	err := wifiDisconnectCmd.RunE(wifiDisconnectCmd, nil)
	var exitErr *exitError
	if !errors.As(err, &exitErr) || exitErr.code != 1 {
		t.Fatalf("expected exitError{code:1}, got: %v", err)
	}
}
