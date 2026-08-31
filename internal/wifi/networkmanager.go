package wifi

import (
	"context"
	"fmt"
	"net"

	"github.com/godbus/dbus/v5"
)

const (
	nmBusName        = "org.freedesktop.NetworkManager"
	nmPath           = "/org/freedesktop/NetworkManager"
	nmInterface      = "org.freedesktop.NetworkManager"
	nmDeviceWifi     = 2   // NM_DEVICE_TYPE_WIFI
	nmStateActivated = 100 // NM_DEVICE_STATE_ACTIVATED
)

// NMManager implementa a interface wifi.Manager utilizando comunicação D-Bus direta.
type NMManager struct {
	conn       *dbus.Conn
	wifiDevice dbus.ObjectPath // Cache do device Wi-Fi descoberto na inicialização
	ifaceName  string          // Nome da interface física (ex: wlan0)
}

// NewNetworkManager inicializa e retorna uma nova instância conectada ao D-Bus do sistema,
// descobrindo e armazenando o caminho do dispositivo Wi-Fi para evitar varreduras repetitivas.
func NewNetworkManager() (*NMManager, error) {
	conn, err := dbus.SystemBus()
	if err != nil {
		return nil, fmt.Errorf("falha ao conectar ao D-Bus do sistema: %w", err)
	}

	m := &NMManager{conn: conn}
	if err := m.discoverWiFiDevice(); err != nil {
		return nil, err
	}

	return m, nil
}

// discoverWiFiDevice localiza o primeiro dispositivo Wi-Fi disponível no NetworkManager e cacheia seu path.
func (m *NMManager) discoverWiFiDevice() error {
	obj := m.conn.Object(nmBusName, nmPath)

	var devicePaths []dbus.ObjectPath
	err := obj.Call(nmInterface+".GetAllDevices", 0).Store(&devicePaths)
	if err != nil {
		return fmt.Errorf("falha ao listar dispositivos via GetAllDevices: %w", err)
	}

	for _, path := range devicePaths {
		devObj := m.conn.Object(nmBusName, path)

		v, err := devObj.GetProperty("org.freedesktop.NetworkManager.Device.DeviceType")
		if err != nil {
			continue
		}
		var devType uint32
		if err := v.Store(&devType); err != nil || devType != nmDeviceWifi {
			continue
		}

		m.wifiDevice = path

		if ifaceVar, err := devObj.GetProperty("org.freedesktop.NetworkManager.Device.Interface"); err == nil {
			_ = ifaceVar.Store(&m.ifaceName)
		}

		return nil
	}

	return fmt.Errorf("nenhum adaptador Wi-Fi compatível encontrado no sistema")
}

// findKnownConnection procura um perfil salvo para o SSID fornecido.
// Retorna o ObjectPath do perfil se encontrado, um booleano indicando sucesso e um erro opcional.
func (m *NMManager) findKnownConnection(ssid string) (dbus.ObjectPath, bool, error) {
	settingsObj := m.conn.Object(nmBusName, "/org/freedesktop/NetworkManager/Settings")
	var connPaths []dbus.ObjectPath
	err := settingsObj.Call("org.freedesktop.NetworkManager.Settings.ListConnections", 0).Store(&connPaths)
	if err != nil {
		return "", false, err
	}

	for _, cp := range connPaths {
		cObj := m.conn.Object(nmBusName, cp)
		var settings map[string]map[string]dbus.Variant
		err := cObj.Call("org.freedesktop.NetworkManager.Settings.Connection.GetSettings", 0).Store(&settings)
		if err != nil {
			continue
		}

		if wifiSec, ok := settings["wifi"]; ok {
			if ssidVar, exists := wifiSec["ssid"]; exists {
				var savedSSID []byte
				if err := ssidVar.Store(&savedSSID); err == nil && string(savedSSID) == ssid {
					return cp, true, nil
				}
			}
		}
	}

	return "", false, nil
}

// List consulta o NetworkManager via D-Bus para obter os Access Points disponíveis.
func (m *NMManager) List(ctx context.Context) ([]AccessPoint, error) {
	if m.wifiDevice == "" {
		return nil, fmt.Errorf("dispositivo Wi-Fi não inicializado")
	}

	devObj := m.conn.Object(nmBusName, m.wifiDevice)

	var apPaths []dbus.ObjectPath
	err := devObj.Call("org.freedesktop.NetworkManager.Device.Wireless.GetAccessPoints", 0).Store(&apPaths)
	if err != nil {
		return nil, fmt.Errorf("falha ao consultar Access Points do dispositivo Wi-Fi: %w", err)
	}

	var accessPoints []AccessPoint

	for _, apPath := range apPaths {
		apObj := m.conn.Object(nmBusName, apPath)

		vSsid, err := apObj.GetProperty("org.freedesktop.NetworkManager.AccessPoint.Ssid")
		if err != nil {
			continue
		}
		var ssidBytes []byte
		if err := vSsid.Store(&ssidBytes); err != nil || len(ssidBytes) == 0 {
			continue
		}
		ssidStr := string(ssidBytes)

		vStrength, err := apObj.GetProperty("org.freedesktop.NetworkManager.AccessPoint.Strength")
		if err != nil {
			continue
		}
		var strength uint8
		if err := vStrength.Store(&strength); err != nil {
			continue
		}

		var frequency uint32
		if vFreq, err := apObj.GetProperty("org.freedesktop.NetworkManager.AccessPoint.Frequency"); err == nil {
			_ = vFreq.Store(&frequency)
		}

		var hwAddress string
		if vHw, err := apObj.GetProperty("org.freedesktop.NetworkManager.AccessPoint.HwAddress"); err == nil {
			_ = vHw.Store(&hwAddress)
		}

		var wpaFlags, rsnFlags uint32
		if vWpa, err := apObj.GetProperty("org.freedesktop.NetworkManager.AccessPoint.WpaFlags"); err == nil {
			_ = vWpa.Store(&wpaFlags)
		}
		if vRsn, err := apObj.GetProperty("org.freedesktop.NetworkManager.AccessPoint.RsnFlags"); err == nil {
			_ = vRsn.Store(&rsnFlags)
		}

		secured := (wpaFlags != 0 || rsnFlags != 0)

		// Utiliza a função unificada de verificação de perfil
		_, known, _ := m.findKnownConnection(ssidStr)

		accessPoints = append(accessPoints, AccessPoint{
			SSID:      ssidStr,
			BSSID:     hwAddress,
			Strength:  strength,
			Frequency: frequency,
			Secured:   secured,
			Known:     known,
		})
	}

	return accessPoints, nil
}

// Connect realiza a conexão com uma rede Wi-Fi, aguardando o estado de ativação real (Activated).
func (m *NMManager) Connect(ctx context.Context, ssid string, password string) error {
	devObj := m.conn.Object(nmBusName, m.wifiDevice)
	var apPaths []dbus.ObjectPath
	if err := devObj.Call("org.freedesktop.NetworkManager.Device.Wireless.GetAccessPoints", 0).Store(&apPaths); err != nil {
		return fmt.Errorf("falha ao escanear APs para conexão: %w", err)
	}

	var targetAPPath dbus.ObjectPath
	var isSecuredAP bool
	for _, apPath := range apPaths {
		apObj := m.conn.Object(nmBusName, apPath)
		vSsid, err := apObj.GetProperty("org.freedesktop.NetworkManager.AccessPoint.Ssid")
		if err != nil {
			continue
		}
		var ssidBytes []byte
		if err := vSsid.Store(&ssidBytes); err == nil && string(ssidBytes) == ssid {
			targetAPPath = apPath
			
			var wpaFlags, rsnFlags uint32
			if vWpa, err := apObj.GetProperty("org.freedesktop.NetworkManager.AccessPoint.WpaFlags"); err == nil {
				_ = vWpa.Store(&wpaFlags)
			}
			if vRsn, err := apObj.GetProperty("org.freedesktop.NetworkManager.AccessPoint.RsnFlags"); err == nil {
				_ = vRsn.Store(&rsnFlags)
			}
			isSecuredAP = (wpaFlags != 0 || rsnFlags != 0)
			break
		}
	}

	if targetAPPath == "" {
		return fmt.Errorf("rede Wi-Fi '%s' não encontrada no alcance atual", ssid)
	}

	// Subscrição e registro de sinais D-Bus usando os métodos nativos corretos do godbus/v5
	if err := m.conn.AddMatchSignal(
		dbus.WithMatchInterface("org.freedesktop.NetworkManager.Device"),
		dbus.WithMatchObjectPath(m.wifiDevice),
		dbus.WithMatchMember("StateChanged"),
	); err != nil {
		return fmt.Errorf("falha ao registrar listener de eventos D-Bus: %w", err)
	}
	defer m.conn.RemoveMatchSignal(
		dbus.WithMatchInterface("org.freedesktop.NetworkManager.Device"),
		dbus.WithMatchObjectPath(m.wifiDevice),
		dbus.WithMatchMember("StateChanged"),
	)

	sigChan := make(chan *dbus.Signal, 10)
	m.conn.Signal(sigChan)
	defer m.conn.RemoveSignal(sigChan)

	nmObj := m.conn.Object(nmBusName, nmPath)

	// Busca unificada se já existe perfil salvo
	savedConnPath, known, err := m.findKnownConnection(ssid)
	if err != nil {
		return fmt.Errorf("falha ao consultar perfis salvos: %w", err)
	}

	if known {
		// Ativa perfil existente reutilizando o ObjectPath descoberto
		if err := nmObj.Call("org.freedesktop.NetworkManager.ActivateConnection", 0, savedConnPath, m.wifiDevice, dbus.ObjectPath("/")).Store(); err != nil {
			return fmt.Errorf("falha ao ativar conexão existente: %w", err)
		}
	} else {
		// Perfil novo: diferencia rede protegida (WPA/WPA2) de rede aberta
		profile := map[string]map[string]dbus.Variant{
			"connection": {
				"id":   dbus.MakeVariant(ssid),
				"type": dbus.MakeVariant("802-11-wireless"),
			},
			"802-11-wireless": {
				"ssid": dbus.MakeVariant([]byte(ssid)),
				"mode": dbus.MakeVariant("infrastructure"),
			},
			"ipv4": {
				"method": dbus.MakeVariant("auto"),
			},
			"ipv6": {
				"method": dbus.MakeVariant("ignore"),
			},
		}

		if isSecuredAP {
			if password == "" {
				return fmt.Errorf("a rede '%s' é protegida por senha, mas nenhuma senha foi fornecida", ssid)
			}
			profile["802-11-wireless-security"] = map[string]dbus.Variant{
				"key-mgmt": dbus.MakeVariant("wpa-psk"),
				"psk":      dbus.MakeVariant(password),
			}
		}

		var activeConn, addedConn dbus.ObjectPath
		if err := nmObj.Call("org.freedesktop.NetworkManager.AddAndActivateConnection", 0, profile, m.wifiDevice, targetAPPath).Store(&activeConn, &addedConn); err != nil {
			return fmt.Errorf("falha ao adicionar e ativar perfil de rede: %w", err)
		}
	}

	// Loop de confirmação síncrona com checagem rigorosa do Path e contexto
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("tempo limite esgotado ou operação cancelada ao aguardar conexão: %w", ctx.Err())
		case sig := <-sigChan:
			if sig.Path != m.wifiDevice {
				continue
			}
			if sig.Name == "org.freedesktop.NetworkManager.Device.StateChanged" && len(sig.Body) >= 2 {
				if newState, ok := sig.Body[0].(uint32); ok {
					if newState == nmStateActivated {
						return nil
					}
					if newState == 20 { // NM_DEVICE_STATE_FAILED
						return fmt.Errorf("falha na autenticação ou estabelecimento da conexão Wi-Fi")
					}
				}
			}
		}
	}
}

// Disconnect desconecta a interface Wi-Fi ativa.
func (m *NMManager) Disconnect(ctx context.Context) error {
	if m.wifiDevice == "" {
		return fmt.Errorf("dispositivo Wi-Fi não inicializado")
	}

	devObj := m.conn.Object(nmBusName, m.wifiDevice)
	
	var state uint32
	if v, err := devObj.GetProperty("org.freedesktop.NetworkManager.Device.State"); err == nil {
		_ = v.Store(&state)
		if state <= 3 {
			return fmt.Errorf("dispositivo Wi-Fi indisponível ou desativado")
		}
		if state < nmStateActivated {
			return fmt.Errorf("o Wi-Fi já se encontra desconectado")
		}
	}

	err := devObj.Call("org.freedesktop.NetworkManager.Device.Disconnect", 0).Store()
	if err != nil {
		return fmt.Errorf("falha ao desconectar dispositivo Wi-Fi: %w", err)
	}

	return nil
}

// Current retorna as informações da conexão Wi-Fi ativa.
func (m *NMManager) Current(ctx context.Context) (Connection, error) {
	if m.wifiDevice == "" {
		return Connection{}, fmt.Errorf("dispositivo Wi-Fi não inicializado")
	}

	devObj := m.conn.Object(nmBusName, m.wifiDevice)

	var activeAPPath dbus.ObjectPath
	if v, err := devObj.GetProperty("org.freedesktop.NetworkManager.Device.Wireless.ActiveAccessPoint"); err == nil {
		_ = v.Store(&activeAPPath)
	}

	if activeAPPath == "/" || activeAPPath == "" {
		return Connection{}, fmt.Errorf("nenhuma rede Wi-Fi ativa encontrada")
	}

	apObj := m.conn.Object(nmBusName, activeAPPath)
	vSsid, err := apObj.GetProperty("org.freedesktop.NetworkManager.AccessPoint.Ssid")
	if err != nil {
		return Connection{}, fmt.Errorf("falha ao ler SSID do AP ativo: %w", err)
	}
	var ssidBytes []byte
	if err := vSsid.Store(&ssidBytes); err != nil || len(ssidBytes) == 0 {
		return Connection{}, fmt.Errorf("SSID ativo corrompido ou vazio")
	}

	var ipAddr net.IP
	var ip4ConfigPath dbus.ObjectPath
	if vIp4, err := devObj.GetProperty("org.freedesktop.NetworkManager.Device.Ip4Config"); err == nil {
		if err := vIp4.Store(&ip4ConfigPath); err == nil && ip4ConfigPath != "/" && ip4ConfigPath != "" {
			ip4Obj := m.conn.Object(nmBusName, ip4ConfigPath)
			if vAddresses, err := ip4Obj.GetProperty("org.freedesktop.NetworkManager.IP4Config.AddressData"); err == nil {
				var addresses []map[string]dbus.Variant
				if err := vAddresses.Store(&addresses); err == nil && len(addresses) > 0 {
					if addrVar, ok := addresses[0]["address"]; ok {
						var ipStr string
						if err := addrVar.Store(&ipStr); err == nil {
							ipAddr = net.ParseIP(ipStr)
						}
					}
				}
			}
		}
	}

	return Connection{
		SSID:      string(ssidBytes),
		Interface: m.ifaceName,
		IPv4:      ipAddr,
	}, nil
}