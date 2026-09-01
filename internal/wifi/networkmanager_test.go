package wifi

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
)

// --- fakes: uma implementação em memória de dbusConn/dbus.BusObject, sem D-Bus real. ---

type fakeCallResult struct {
	body []any
	err  error
}

type fakeBusObject struct {
	path       dbus.ObjectPath
	properties map[string]dbus.Variant
	calls      map[string]fakeCallResult
	callArgs   map[string][]any
}

func newFakeBusObject(path dbus.ObjectPath) *fakeBusObject {
	return &fakeBusObject{
		path:       path,
		properties: map[string]dbus.Variant{},
		calls:      map[string]fakeCallResult{},
		callArgs:   map[string][]any{},
	}
}

func (o *fakeBusObject) withProperty(name string, value any) *fakeBusObject {
	o.properties[name] = dbus.MakeVariant(value)
	return o
}

func (o *fakeBusObject) withCall(method string, err error, body ...any) *fakeBusObject {
	o.calls[method] = fakeCallResult{body: body, err: err}
	return o
}

func (o *fakeBusObject) Call(method string, flags dbus.Flags, args ...any) *dbus.Call {
	o.callArgs[method] = args
	res, ok := o.calls[method]
	if !ok {
		return &dbus.Call{Err: fmt.Errorf("fakeBusObject(%s): unexpected call %s", o.path, method)}
	}
	return &dbus.Call{Body: res.body, Err: res.err}
}

func (o *fakeBusObject) CallWithContext(_ context.Context, method string, flags dbus.Flags, args ...any) *dbus.Call {
	return o.Call(method, flags, args...)
}
func (o *fakeBusObject) Go(string, dbus.Flags, chan *dbus.Call, ...any) *dbus.Call {
	panic("fakeBusObject: Go not implemented")
}
func (o *fakeBusObject) GoWithContext(context.Context, string, dbus.Flags, chan *dbus.Call, ...any) *dbus.Call {
	panic("fakeBusObject: GoWithContext not implemented")
}
func (o *fakeBusObject) AddMatchSignal(string, string, ...dbus.MatchOption) *dbus.Call {
	panic("fakeBusObject: AddMatchSignal not implemented")
}
func (o *fakeBusObject) RemoveMatchSignal(string, string, ...dbus.MatchOption) *dbus.Call {
	panic("fakeBusObject: RemoveMatchSignal not implemented")
}

func (o *fakeBusObject) GetProperty(p string) (dbus.Variant, error) {
	v, ok := o.properties[p]
	if !ok {
		return dbus.Variant{}, fmt.Errorf("fakeBusObject(%s): unknown property %s", o.path, p)
	}
	return v, nil
}
func (o *fakeBusObject) StoreProperty(string, any) error {
	panic("fakeBusObject: StoreProperty not implemented")
}
func (o *fakeBusObject) SetProperty(string, any) error {
	panic("fakeBusObject: SetProperty not implemented")
}
func (o *fakeBusObject) Destination() string   { return nmBusName }
func (o *fakeBusObject) Path() dbus.ObjectPath { return o.path }

type fakeConn struct {
	objects            map[dbus.ObjectPath]*fakeBusObject
	sigChan            chan<- *dbus.Signal
	onSignalRegistered func()
}

func newFakeConn() *fakeConn {
	return &fakeConn{objects: map[dbus.ObjectPath]*fakeBusObject{}}
}

func (c *fakeConn) put(obj *fakeBusObject) *fakeConn {
	c.objects[obj.path] = obj
	return c
}

func (c *fakeConn) Object(dest string, path dbus.ObjectPath) dbus.BusObject {
	if obj, ok := c.objects[path]; ok {
		return obj
	}
	return newFakeBusObject(path)
}

func (c *fakeConn) AddMatchSignal(options ...dbus.MatchOption) error    { return nil }
func (c *fakeConn) RemoveMatchSignal(options ...dbus.MatchOption) error { return nil }
func (c *fakeConn) Signal(ch chan<- *dbus.Signal) {
	c.sigChan = ch
	if c.onSignalRegistered != nil {
		c.onSignalRegistered()
	}
}
func (c *fakeConn) RemoveSignal(ch chan<- *dbus.Signal) {}

// --- helpers para montar um cenário de dispositivo Wi-Fi descoberto ---

const (
	testWifiDevicePath = dbus.ObjectPath("/org/freedesktop/NetworkManager/Devices/3")
	testSettingsPath   = dbus.ObjectPath("/org/freedesktop/NetworkManager/Settings")
)

func newDiscoveredConn() *fakeConn {
	conn := newFakeConn()
	conn.put(newFakeBusObject(nmPath).
		withCall(nmInterface+".GetAllDevices", nil, []dbus.ObjectPath{testWifiDevicePath}))
	conn.put(newFakeBusObject(testWifiDevicePath).
		withProperty("org.freedesktop.NetworkManager.Device.DeviceType", uint32(nmDeviceWifi)).
		withProperty("org.freedesktop.NetworkManager.Device.Interface", "wlan0"))
	conn.put(newFakeBusObject(testSettingsPath).
		withCall("org.freedesktop.NetworkManager.Settings.ListConnections", nil, []dbus.ObjectPath{}))
	return conn
}

// --- testes de isSecuredFromFlags (função pura) ---

func TestIsSecuredFromFlags(t *testing.T) {
	tests := []struct {
		name               string
		wpaFlags, rsnFlags uint32
		want               bool
	}{
		{"nenhuma flag", 0, 0, false},
		{"apenas wpa", 1, 0, true},
		{"apenas rsn", 0, 1, true},
		{"ambas", 1, 1, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isSecuredFromFlags(tt.wpaFlags, tt.rsnFlags); got != tt.want {
				t.Errorf("isSecuredFromFlags(%d, %d) = %v, want %v", tt.wpaFlags, tt.rsnFlags, got, tt.want)
			}
		})
	}
}

// --- testes de discoverWiFiDevice / newNMManager ---

func TestNewNMManager_DiscoversWifiDevice(t *testing.T) {
	conn := newDiscoveredConn()

	m, err := newNMManager(conn)
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if m.wifiDevice != testWifiDevicePath {
		t.Errorf("wifiDevice = %q, want %q", m.wifiDevice, testWifiDevicePath)
	}
	if m.ifaceName != "wlan0" {
		t.Errorf("ifaceName = %q, want wlan0", m.ifaceName)
	}
}

func TestNewNMManager_NoWifiDevice(t *testing.T) {
	conn := newFakeConn()
	conn.put(newFakeBusObject(nmPath).
		withCall(nmInterface+".GetAllDevices", nil, []dbus.ObjectPath{"/dev/eth0"}))
	conn.put(newFakeBusObject("/dev/eth0").
		withProperty("org.freedesktop.NetworkManager.Device.DeviceType", uint32(1))) // NM_DEVICE_TYPE_ETHERNET

	_, err := newNMManager(conn)
	if err == nil {
		t.Fatal("expected error when no Wi-Fi device is present, got nil")
	}
}

func TestNewNMManager_GetAllDevicesError(t *testing.T) {
	conn := newFakeConn()
	conn.put(newFakeBusObject(nmPath).
		withCall(nmInterface+".GetAllDevices", errors.New("dbus down")))

	_, err := newNMManager(conn)
	if err == nil {
		t.Fatal("expected error when GetAllDevices fails, got nil")
	}
}

// --- testes de List ---

func TestList_Success(t *testing.T) {
	conn := newDiscoveredConn()
	const apPath = dbus.ObjectPath("/org/freedesktop/NetworkManager/AccessPoint/1")
	conn.objects[testWifiDevicePath].withCall(
		"org.freedesktop.NetworkManager.Device.Wireless.GetAccessPoints", nil, []dbus.ObjectPath{apPath})
	conn.put(newFakeBusObject(apPath).
		withProperty("org.freedesktop.NetworkManager.AccessPoint.Ssid", []byte("MinhaRede")).
		withProperty("org.freedesktop.NetworkManager.AccessPoint.Strength", uint8(80)).
		withProperty("org.freedesktop.NetworkManager.AccessPoint.Frequency", uint32(2412)).
		withProperty("org.freedesktop.NetworkManager.AccessPoint.HwAddress", "AA:BB:CC:DD:EE:FF").
		withProperty("org.freedesktop.NetworkManager.AccessPoint.WpaFlags", uint32(1)).
		withProperty("org.freedesktop.NetworkManager.AccessPoint.RsnFlags", uint32(0)))

	m, err := newNMManager(conn)
	if err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	aps, err := m.List(context.Background())
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if len(aps) != 1 {
		t.Fatalf("expected 1 access point, got %d", len(aps))
	}
	ap := aps[0]
	if ap.SSID != "MinhaRede" || ap.BSSID != "AA:BB:CC:DD:EE:FF" || ap.Strength != 80 || ap.Frequency != 2412 {
		t.Errorf("unexpected AccessPoint: %+v", ap)
	}
	if !ap.Secured {
		t.Error("expected Secured = true (WpaFlags != 0)")
	}
	if ap.Known {
		t.Error("expected Known = false (no saved connections)")
	}
}

func TestList_DeviceNotInitialized(t *testing.T) {
	m := &NMManager{conn: newFakeConn()}
	_, err := m.List(context.Background())
	if err == nil {
		t.Fatal("expected error when wifiDevice is empty, got nil")
	}
}

func TestList_GetAccessPointsError(t *testing.T) {
	conn := newDiscoveredConn()
	conn.objects[testWifiDevicePath].withCall(
		"org.freedesktop.NetworkManager.Device.Wireless.GetAccessPoints", errors.New("scan failed"))

	m, err := newNMManager(conn)
	if err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	_, err = m.List(context.Background())
	if err == nil {
		t.Fatal("expected error when GetAccessPoints fails, got nil")
	}
}

func TestList_SkipsAPsWithoutSSID(t *testing.T) {
	conn := newDiscoveredConn()
	const apPath = dbus.ObjectPath("/org/freedesktop/NetworkManager/AccessPoint/2")
	conn.objects[testWifiDevicePath].withCall(
		"org.freedesktop.NetworkManager.Device.Wireless.GetAccessPoints", nil, []dbus.ObjectPath{apPath})
	// AP sem a propriedade Ssid configurada — GetProperty falha, deve ser ignorado.
	conn.put(newFakeBusObject(apPath))

	m, err := newNMManager(conn)
	if err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	aps, err := m.List(context.Background())
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if len(aps) != 0 {
		t.Errorf("expected 0 access points, got %d", len(aps))
	}
}

// --- testes de Connect ---

func setupConnectScenario(t *testing.T, ssid string, secured bool) (*fakeConn, dbus.ObjectPath) {
	t.Helper()
	conn := newDiscoveredConn()
	apPath := dbus.ObjectPath("/org/freedesktop/NetworkManager/AccessPoint/9")
	conn.objects[testWifiDevicePath].withCall(
		"org.freedesktop.NetworkManager.Device.Wireless.GetAccessPoints", nil, []dbus.ObjectPath{apPath})

	var wpa uint32
	if secured {
		wpa = 1
	}
	conn.put(newFakeBusObject(apPath).
		withProperty("org.freedesktop.NetworkManager.AccessPoint.Ssid", []byte(ssid)).
		withProperty("org.freedesktop.NetworkManager.AccessPoint.WpaFlags", wpa).
		withProperty("org.freedesktop.NetworkManager.AccessPoint.RsnFlags", uint32(0)))

	return conn, apPath
}

// sendStateChanged envia, assim que Connect registra o canal de sinais, um StateChanged
// para o dispositivo Wi-Fi com o estado informado.
func sendStateChangedWhenReady(conn *fakeConn, state uint32) {
	conn.onSignalRegistered = func() {
		go func() {
			conn.sigChan <- &dbus.Signal{
				Path: testWifiDevicePath,
				Name: "org.freedesktop.NetworkManager.Device.StateChanged",
				Body: []any{state, uint32(0), uint32(0)},
			}
		}()
	}
}

func TestConnect_NewOpenNetwork_Success(t *testing.T) {
	conn, _ := setupConnectScenario(t, "RedeAberta", false)
	conn.objects[nmPath].withCall(
		"org.freedesktop.NetworkManager.AddAndActivateConnection", nil,
		dbus.ObjectPath("/active/1"), dbus.ObjectPath("/added/1"))
	sendStateChangedWhenReady(conn, nmStateActivated)

	m, err := newNMManager(conn)
	if err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := m.Connect(ctx, "RedeAberta", ""); err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
}

func TestConnect_NewSecuredNetwork_RequiresPassword(t *testing.T) {
	conn, _ := setupConnectScenario(t, "RedeSegura", true)

	m, err := newNMManager(conn)
	if err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	err = m.Connect(context.Background(), "RedeSegura", "")
	if err == nil {
		t.Fatal("expected error when connecting to a secured network without a password, got nil")
	}
}

func TestConnect_NewSecuredNetwork_Success(t *testing.T) {
	conn, _ := setupConnectScenario(t, "RedeSegura", true)
	conn.objects[nmPath].withCall(
		"org.freedesktop.NetworkManager.AddAndActivateConnection", nil,
		dbus.ObjectPath("/active/2"), dbus.ObjectPath("/added/2"))
	sendStateChangedWhenReady(conn, nmStateActivated)

	m, err := newNMManager(conn)
	if err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := m.Connect(ctx, "RedeSegura", "senha-correta"); err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
}

func TestConnect_KnownProfile_ActivatesExisting(t *testing.T) {
	conn, _ := setupConnectScenario(t, "RedeConhecida", false)
	const savedConnPath = dbus.ObjectPath("/org/freedesktop/NetworkManager/Settings/5")
	conn.objects[testSettingsPath].withCall(
		"org.freedesktop.NetworkManager.Settings.ListConnections", nil, []dbus.ObjectPath{savedConnPath})
	settings := map[string]map[string]dbus.Variant{
		"802-11-wireless": {"ssid": dbus.MakeVariant([]byte("RedeConhecida"))},
	}
	conn.put(newFakeBusObject(savedConnPath).
		withCall("org.freedesktop.NetworkManager.Settings.Connection.GetSettings", nil, settings))
	conn.objects[nmPath].withCall("org.freedesktop.NetworkManager.ActivateConnection", nil)
	sendStateChangedWhenReady(conn, nmStateActivated)

	m, err := newNMManager(conn)
	if err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := m.Connect(ctx, "RedeConhecida", ""); err != nil {
		t.Fatalf("expected success activating known profile, got error: %v", err)
	}
}

// TestConnect_KnownProfile_UpdatesPasswordWhenProvided é uma regressão do bug em que
// reativar um perfil salvo ignorava silenciosamente uma senha nova digitada pelo usuário,
// fazendo "wifi connect" falhar indefinidamente após a senha da rede mudar.
func TestConnect_KnownProfile_UpdatesPasswordWhenProvided(t *testing.T) {
	conn, _ := setupConnectScenario(t, "RedeConhecida", true)
	const savedConnPath = dbus.ObjectPath("/org/freedesktop/NetworkManager/Settings/5")
	conn.objects[testSettingsPath].withCall(
		"org.freedesktop.NetworkManager.Settings.ListConnections", nil, []dbus.ObjectPath{savedConnPath})
	settings := map[string]map[string]dbus.Variant{
		"802-11-wireless": {"ssid": dbus.MakeVariant([]byte("RedeConhecida"))},
	}
	savedConnObj := newFakeBusObject(savedConnPath).
		withCall("org.freedesktop.NetworkManager.Settings.Connection.GetSettings", nil, settings).
		withCall("org.freedesktop.NetworkManager.Settings.Connection.Update", nil)
	conn.put(savedConnObj)
	conn.objects[nmPath].withCall("org.freedesktop.NetworkManager.ActivateConnection", nil)
	sendStateChangedWhenReady(conn, nmStateActivated)

	m, err := newNMManager(conn)
	if err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := m.Connect(ctx, "RedeConhecida", "senha-nova"); err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}

	args, called := savedConnObj.callArgs["org.freedesktop.NetworkManager.Settings.Connection.Update"]
	if !called {
		t.Fatal("expected Settings.Connection.Update to be called with the new password")
	}
	updatedSettings, ok := args[0].(map[string]map[string]dbus.Variant)
	if !ok {
		t.Fatalf("expected Update's argument to be a settings map, got %T", args[0])
	}
	var psk string
	if err := updatedSettings["802-11-wireless-security"]["psk"].Store(&psk); err != nil || psk != "senha-nova" {
		t.Errorf("expected updated psk = %q, got %q (err=%v)", "senha-nova", psk, err)
	}
}

// TestConnect_DisconnectedDuringAttemptIsFailure cobre o caso em que o NetworkManager volta
// para "disconnected" (sem nunca emitir explicitamente NM_DEVICE_STATE_FAILED) após rejeitar
// a conexão — antes tratado apenas pelo timeout de 15s do contexto, com mensagem genérica.
func TestConnect_DisconnectedDuringAttemptIsFailure(t *testing.T) {
	conn, _ := setupConnectScenario(t, "RedeAberta", false)
	conn.objects[nmPath].withCall(
		"org.freedesktop.NetworkManager.AddAndActivateConnection", nil,
		dbus.ObjectPath("/active/5"), dbus.ObjectPath("/added/5"))
	sendStateChangedWhenReady(conn, nmStateDisconnected)

	m, err := newNMManager(conn)
	if err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	start := time.Now()
	err = m.Connect(ctx, "RedeAberta", "")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error when NetworkManager reverts to DISCONNECTED during a connect attempt, got nil")
	}
	if elapsed > time.Second {
		t.Errorf("expected fast failure detection, took %v (should not wait for the 2s context timeout)", elapsed)
	}
}

func TestConnect_APNotFound(t *testing.T) {
	conn := newDiscoveredConn()
	conn.objects[testWifiDevicePath].withCall(
		"org.freedesktop.NetworkManager.Device.Wireless.GetAccessPoints", nil, []dbus.ObjectPath{})

	m, err := newNMManager(conn)
	if err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	err = m.Connect(context.Background(), "RedeInexistente", "")
	if err == nil {
		t.Fatal("expected error when target AP is not found, got nil")
	}
}

func TestConnect_StateChangedFailed(t *testing.T) {
	conn, _ := setupConnectScenario(t, "RedeAberta", false)
	conn.objects[nmPath].withCall(
		"org.freedesktop.NetworkManager.AddAndActivateConnection", nil,
		dbus.ObjectPath("/active/3"), dbus.ObjectPath("/added/3"))
	sendStateChangedWhenReady(conn, nmStateFailed)

	m, err := newNMManager(conn)
	if err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err = m.Connect(ctx, "RedeAberta", "")
	if err == nil {
		t.Fatal("expected error when NetworkManager reports NM_DEVICE_STATE_FAILED, got nil")
	}
}

func TestConnect_ContextTimeout(t *testing.T) {
	conn, _ := setupConnectScenario(t, "RedeAberta", false)
	conn.objects[nmPath].withCall(
		"org.freedesktop.NetworkManager.AddAndActivateConnection", nil,
		dbus.ObjectPath("/active/4"), dbus.ObjectPath("/added/4"))
	// Nenhum sinal é enviado — o contexto deve expirar primeiro.

	m, err := newNMManager(conn)
	if err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err = m.Connect(ctx, "RedeAberta", "")
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected wrapped context.DeadlineExceeded, got: %v", err)
	}
}

// --- testes de Disconnect ---

func TestDisconnect_Success(t *testing.T) {
	conn := newDiscoveredConn()
	conn.objects[testWifiDevicePath].
		withProperty("org.freedesktop.NetworkManager.Device.State", uint32(nmStateActivated)).
		withCall("org.freedesktop.NetworkManager.Device.Disconnect", nil)

	m, err := newNMManager(conn)
	if err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	if err := m.Disconnect(context.Background()); err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
}

func TestDisconnect_DeviceUnavailable(t *testing.T) {
	conn := newDiscoveredConn()
	conn.objects[testWifiDevicePath].
		withProperty("org.freedesktop.NetworkManager.Device.State", uint32(nmStateUnavailable))

	m, err := newNMManager(conn)
	if err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	if err := m.Disconnect(context.Background()); err == nil {
		t.Fatal("expected error when device is unavailable, got nil")
	}
}

func TestDisconnect_AlreadyDisconnected(t *testing.T) {
	conn := newDiscoveredConn()
	// Entre "unavailable" (20) e "activated" (100): já desconectado, mas disponível.
	conn.objects[testWifiDevicePath].
		withProperty("org.freedesktop.NetworkManager.Device.State", uint32(30))

	m, err := newNMManager(conn)
	if err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	if err := m.Disconnect(context.Background()); err == nil {
		t.Fatal("expected error when already disconnected, got nil")
	}
}

func TestDisconnect_NotInitialized(t *testing.T) {
	m := &NMManager{conn: newFakeConn()}
	if err := m.Disconnect(context.Background()); err == nil {
		t.Fatal("expected error when wifiDevice is empty, got nil")
	}
}

func TestDisconnect_CallFails(t *testing.T) {
	conn := newDiscoveredConn()
	conn.objects[testWifiDevicePath].
		withProperty("org.freedesktop.NetworkManager.Device.State", uint32(nmStateActivated)).
		withCall("org.freedesktop.NetworkManager.Device.Disconnect", errors.New("dbus rejected"))

	m, err := newNMManager(conn)
	if err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	if err := m.Disconnect(context.Background()); err == nil {
		t.Fatal("expected error when Disconnect D-Bus call fails, got nil")
	}
}

// --- testes de Current ---

func TestCurrent_NoActiveConnection(t *testing.T) {
	conn := newDiscoveredConn()
	conn.objects[testWifiDevicePath].
		withProperty("org.freedesktop.NetworkManager.Device.Wireless.ActiveAccessPoint", dbus.ObjectPath("/"))

	m, err := newNMManager(conn)
	if err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	_, err = m.Current(context.Background())
	if !errors.Is(err, ErrNoActiveConnection) {
		t.Fatalf("expected ErrNoActiveConnection, got: %v", err)
	}
}

func TestCurrent_Success(t *testing.T) {
	conn := newDiscoveredConn()
	const apPath = dbus.ObjectPath("/org/freedesktop/NetworkManager/AccessPoint/7")
	const ip4ConfigPath = dbus.ObjectPath("/org/freedesktop/NetworkManager/IP4Config/1")

	conn.objects[testWifiDevicePath].
		withProperty("org.freedesktop.NetworkManager.Device.Wireless.ActiveAccessPoint", apPath).
		withProperty("org.freedesktop.NetworkManager.Device.Ip4Config", ip4ConfigPath)

	conn.put(newFakeBusObject(apPath).
		withProperty("org.freedesktop.NetworkManager.AccessPoint.Ssid", []byte("RedeAtual")))

	addresses := []map[string]dbus.Variant{
		{"address": dbus.MakeVariant("192.168.1.50")},
	}
	conn.put(newFakeBusObject(ip4ConfigPath).
		withProperty("org.freedesktop.NetworkManager.IP4Config.AddressData", addresses))

	m, err := newNMManager(conn)
	if err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	current, err := m.Current(context.Background())
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if current.SSID != "RedeAtual" {
		t.Errorf("SSID = %q, want RedeAtual", current.SSID)
	}
	if current.Interface != "wlan0" {
		t.Errorf("Interface = %q, want wlan0", current.Interface)
	}
	if current.IPv4.String() != "192.168.1.50" {
		t.Errorf("IPv4 = %v, want 192.168.1.50", current.IPv4)
	}
}

func TestCurrent_NotInitialized(t *testing.T) {
	m := &NMManager{conn: newFakeConn()}
	_, err := m.Current(context.Background())
	if err == nil {
		t.Fatal("expected error when wifiDevice is empty, got nil")
	}
}

func TestCurrent_CorruptSSID(t *testing.T) {
	conn := newDiscoveredConn()
	const apPath = dbus.ObjectPath("/org/freedesktop/NetworkManager/AccessPoint/8")
	conn.objects[testWifiDevicePath].
		withProperty("org.freedesktop.NetworkManager.Device.Wireless.ActiveAccessPoint", apPath)
	conn.put(newFakeBusObject(apPath).
		withProperty("org.freedesktop.NetworkManager.AccessPoint.Ssid", []byte{}))

	m, err := newNMManager(conn)
	if err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	_, err = m.Current(context.Background())
	if err == nil {
		t.Fatal("expected error for empty SSID, got nil")
	}
}
