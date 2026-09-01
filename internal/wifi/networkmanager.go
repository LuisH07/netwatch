package wifi

import (
	"context"
	"errors"
	"fmt"
	"net"

	"github.com/godbus/dbus/v5"
)

// ErrNoActiveConnection indica que não há nenhuma rede Wi-Fi ativa no momento —
// não é uma falha de D-Bus/NetworkManager, apenas um estado esperado do dispositivo.
var ErrNoActiveConnection = errors.New("nenhuma rede Wi-Fi ativa encontrada")

const (
	nmBusName           = "org.freedesktop.NetworkManager"
	nmPath              = "/org/freedesktop/NetworkManager"
	nmInterface         = "org.freedesktop.NetworkManager"
	nmDeviceWifi        = 2   // NM_DEVICE_TYPE_WIFI
	nmStateUnavailable  = 20  // NM_DEVICE_STATE_UNAVAILABLE
	nmStateDisconnected = 30  // NM_DEVICE_STATE_DISCONNECTED
	nmStateActivated    = 100 // NM_DEVICE_STATE_ACTIVATED
	nmStateFailed       = 120 // NM_DEVICE_STATE_FAILED
)

// dbusConn é o subconjunto de *dbus.Conn usado por NMManager, extraído para permitir
// injetar uma conexão fake em testes sem depender de um barramento D-Bus real.
type dbusConn interface {
	Object(dest string, path dbus.ObjectPath) dbus.BusObject
	AddMatchSignal(options ...dbus.MatchOption) error
	RemoveMatchSignal(options ...dbus.MatchOption) error
	Signal(ch chan<- *dbus.Signal)
	RemoveSignal(ch chan<- *dbus.Signal)
}

// NMManager implementa a interface wifi.Manager utilizando comunicação D-Bus direta.
type NMManager struct {
	conn       dbusConn
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

	return newNMManager(conn)
}

// newNMManager constrói um NMManager a partir de uma dbusConn já estabelecida — separado
// de NewNetworkManager para permitir testes com uma conexão fake.
func newNMManager(conn dbusConn) (*NMManager, error) {
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

		// GetSettings retorna a seção Wi-Fi sob a chave canônica "802-11-wireless" — "wifi"
		// é apenas um alias aceito por algumas APIs de mais alto nível (ex.: nmcli), nunca
		// o nome usado pelo D-Bus GetSettings. Checar apenas "wifi" fazia essa função nunca
		// encontrar um perfil salvo, tratando toda rede como "desconhecida" indefinidamente.
		wifiSec, ok := settings["802-11-wireless"]
		if !ok {
			continue
		}
		ssidVar, exists := wifiSec["ssid"]
		if !exists {
			continue
		}
		var savedSSID []byte
		if err := ssidVar.Store(&savedSSID); err == nil && string(savedSSID) == ssid {
			return cp, true, nil
		}
	}

	return "", false, nil
}

// updateConnectionPassword sobrescreve a senha (PSK) de um perfil de conexão já salvo.
// Necessário para que reativar um perfil conhecido respeite uma senha nova digitada pelo
// usuário, em vez de sempre reusar a senha antiga armazenada — caso contrário "wifi connect"
// nunca se recupera de uma senha trocada.
func (m *NMManager) updateConnectionPassword(connPath dbus.ObjectPath, password string) error {
	connObj := m.conn.Object(nmBusName, connPath)

	var settings map[string]map[string]dbus.Variant
	if err := connObj.Call("org.freedesktop.NetworkManager.Settings.Connection.GetSettings", 0).Store(&settings); err != nil {
		return fmt.Errorf("falha ao ler configurações salvas: %w", err)
	}

	// Reconstrói apenas as seções necessárias em vez de reenviar o blob inteiro devolvido
	// por GetSettings: propriedades complexas (ex.: ipv6.addresses, do tipo D-Bus
	// "a(ayuay)") perdem a assinatura de tipo original ao passar por dbus.Variant genérico
	// no Go — reenviá-las via Update() faz o NetworkManager rejeitar a chamada com um erro
	// como "can't set property of type 'a(ayuay)' from value of type 'aav'". As seções
	// "connection" e "802-11-wireless" só têm propriedades escalares/arrays simples e podem
	// ser repassadas com segurança; ipv4/ipv6 são reconstruídas do zero (mesma suposição de
	// DHCP já usada ao criar um perfil novo em Connect(), acima) em vez de tentar preservar
	// configuração estática de IP que o restante do código também não suporta.
	update := map[string]map[string]dbus.Variant{
		"connection":      settings["connection"],
		"802-11-wireless": settings["802-11-wireless"],
		"802-11-wireless-security": {
			"key-mgmt": dbus.MakeVariant("wpa-psk"),
			"psk":      dbus.MakeVariant(password),
		},
		"ipv4": {"method": dbus.MakeVariant("auto")},
		"ipv6": {"method": dbus.MakeVariant("ignore")},
	}

	return connObj.Call("org.freedesktop.NetworkManager.Settings.Connection.Update", 0, update).Store()
}

// isSecuredFromFlags reporta se um Access Point é protegido com base em suas flags WPA/RSN.
// Função pura, sem I/O, para poder ser testada isoladamente das chamadas D-Bus.
func isSecuredFromFlags(wpaFlags, rsnFlags uint32) bool {
	return wpaFlags != 0 || rsnFlags != 0
}

// apSecurityFlags lê as flags WPA/RSN de um Access Point via D-Bus.
func (m *NMManager) apSecurityFlags(apObj dbus.BusObject) (wpaFlags, rsnFlags uint32) {
	if v, err := apObj.GetProperty("org.freedesktop.NetworkManager.AccessPoint.WpaFlags"); err == nil {
		_ = v.Store(&wpaFlags)
	}
	if v, err := apObj.GetProperty("org.freedesktop.NetworkManager.AccessPoint.RsnFlags"); err == nil {
		_ = v.Store(&rsnFlags)
	}
	return wpaFlags, rsnFlags
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

		wpaFlags, rsnFlags := m.apSecurityFlags(apObj)
		secured := isSecuredFromFlags(wpaFlags, rsnFlags)

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

			wpaFlags, rsnFlags := m.apSecurityFlags(apObj)
			isSecuredAP = isSecuredFromFlags(wpaFlags, rsnFlags)
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
		// Se uma senha foi fornecida, atualiza as credenciais do perfil salvo antes de
		// ativar — sem isso, reativar um perfil existente ignorava silenciosamente
		// qualquer senha nova digitada pelo usuário, fazendo "wifi connect" falhar
		// indefinidamente sempre que a senha da rede mudasse.
		if password != "" {
			if err := m.updateConnectionPassword(savedConnPath, password); err != nil {
				return fmt.Errorf("falha ao atualizar credenciais salvas: %w", err)
			}
		}
		// Ativa perfil existente reutilizando o ObjectPath descoberto. ActivateConnection
		// retorna um parâmetro de saída (o path da conexão ativa) — Store() precisa de um
		// destino para ele, senão falha com "length mismatch" antes mesmo de tentar ativar.
		var activatedConn dbus.ObjectPath
		if err := nmObj.Call("org.freedesktop.NetworkManager.ActivateConnection", 0, savedConnPath, m.wifiDevice, dbus.ObjectPath("/")).Store(&activatedConn); err != nil {
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
					if newState == nmStateFailed {
						return fmt.Errorf("falha na autenticação ou estabelecimento da conexão Wi-Fi (estado NetworkManager: %d)", newState)
					}
					// Um estado <= DISCONNECTED alcançado enquanto aguardamos ativação também indica
					// falha real (ex.: o driver desiste após rejeitar a senha e volta direto para
					// "disconnected" sem passar por "failed") — sem isso, esses casos só eram
					// detectados 15s depois, no timeout do contexto, com mensagem genérica.
					//
					// Exceção: se já estávamos conectados a OUTRA rede, o NetworkManager primeiro
					// desativa essa rede anterior (old_state ACTIVATED -> new_state DISCONNECTED)
					// antes mesmo de começar a tentar ativar a rede alvo. Esse teardown esperado não
					// pode ser confundido com uma falha da nova tentativa de conexão.
					if newState <= nmStateDisconnected {
						oldState, _ := sig.Body[1].(uint32)
						if oldState == nmStateActivated {
							continue
						}
						return fmt.Errorf("falha na autenticação ou estabelecimento da conexão Wi-Fi (estado NetworkManager: %d)", newState)
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
		if state <= nmStateUnavailable {
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
		return Connection{}, ErrNoActiveConnection
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
