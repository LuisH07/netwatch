package wifi

import (
	"context"
	"fmt"
	"net"

	"github.com/godbus/dbus/v5"
)

const (
	nmBusName    = "org.freedesktop.NetworkManager"
	nmPath       = "/org/freedesktop/NetworkManager"
	nmInterface  = "org.freedesktop.NetworkManager"
	nmDeviceWifi = 2 // NM_DEVICE_TYPE_WIFI
)

// NMManager implementa a interface wifi.Manager utilizando comunicação D-Bus direta.
type NMManager struct {
	conn *dbus.Conn
}

// NewNetworkManager inicializa e retorna uma nova instância conectada ao D-Bus do sistema.
func NewNetworkManager() (*NMManager, error) {
	conn, err := dbus.SystemBus()
	if err != nil {
		return nil, fmt.Errorf("falha ao conectar ao D-Bus do sistema: %w", err)
	}
	return &NMManager{conn: conn}, nil
}

// List consulta o NetworkManager via D-Bus para obter os dispositivos Wi-Fi e seus Access Points disponíveis.
func (m *NMManager) List(ctx context.Context) ([]AccessPoint, error) {
	obj := m.conn.Object(nmBusName, nmPath)

	var devicePaths []dbus.ObjectPath
	err := obj.Call(nmInterface+".GetDevices", 0).Store(&devicePaths)
	if err != nil {
		return nil, fmt.Errorf("falha ao listar dispositivos do NetworkManager: %w", err)
	}

	var accessPoints []AccessPoint

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

		var apPaths []dbus.ObjectPath
		err = devObj.Call("org.freedesktop.NetworkManager.Device.Wireless.GetAccessPoints", 0).Store(&apPaths)
		if err != nil {
			continue
		}

		for _, apPath := range apPaths {
			apObj := m.conn.Object(nmBusName, apPath)

			var ssidBytes []byte
			if v, err := apObj.GetProperty("org.freedesktop.NetworkManager.AccessPoint.Ssid"); err == nil {
				_ = v.Store(&ssidBytes)
			}
			if len(ssidBytes) == 0 {
				continue
			}

			var strength uint8
			if v, err := apObj.GetProperty("org.freedesktop.NetworkManager.AccessPoint.Strength"); err == nil {
				_ = v.Store(&strength)
			}

			var frequency uint32
			if v, err := apObj.GetProperty("org.freedesktop.NetworkManager.AccessPoint.Frequency"); err == nil {
				_ = v.Store(&frequency)
			}

			var hwAddress string
			if v, err := apObj.GetProperty("org.freedesktop.NetworkManager.AccessPoint.HwAddress"); err == nil {
				_ = v.Store(&hwAddress)
			}

			var wpaFlags, rsnFlags uint32
			if v, err := apObj.GetProperty("org.freedesktop.NetworkManager.AccessPoint.WpaFlags"); err == nil {
				_ = v.Store(&wpaFlags)
			}
			if v, err := apObj.GetProperty("org.freedesktop.NetworkManager.AccessPoint.RsnFlags"); err == nil {
				_ = v.Store(&rsnFlags)
			}

			secured := (wpaFlags != 0 || rsnFlags != 0)

			accessPoints = append(accessPoints, AccessPoint{
				SSID:      string(ssidBytes),
				BSSID:     hwAddress,
				Strength:  strength,
				Frequency: frequency,
				Secured:   secured,
			})
		}
	}

	return accessPoints, nil
}

// Connect realiza a conexão com uma rede Wi-Fi utilizando o SSID e a senha fornecidos.
func (m *NMManager) Connect(ctx context.Context, ssid string, password string) error {
	return fmt.Errorf("conexão direta via CLI requer mapeamento de perfil D-Bus add-and-activate")
}

// Disconnect desconecta a interface Wi-Fi ativa atual chamando o método Device.Disconnect.
func (m *NMManager) Disconnect(ctx context.Context) error {
	obj := m.conn.Object(nmBusName, nmPath)

	var devicePaths []dbus.ObjectPath
	err := obj.Call(nmInterface+".GetDevices", 0).Store(&devicePaths)
	if err != nil {
		return fmt.Errorf("falha ao buscar dispositivos para desconexão: %w", err)
	}

	for _, path := range devicePaths {
		devObj := m.conn.Object(nmBusName, path)
		var devType uint32
		if v, err := devObj.GetProperty("org.freedesktop.NetworkManager.Device.DeviceType"); err == nil {
			_ = v.Store(&devType)
			if devType == nmDeviceWifi {
				err = devObj.Call("org.freedesktop.NetworkManager.Device.Disconnect", 0).Store()
				if err != nil {
					return fmt.Errorf("falha ao desconectar dispositivo Wi-Fi: %w", err)
				}
				return nil
			}
		}
	}

	return fmt.Errorf("nenhum dispositivo Wi-Fi encontrado para desconectar")
}

// Current retorna as informações da conexão Wi-Fi ativa no momento inspecionando o ActiveAccessPoint do dispositivo.
func (m *NMManager) Current(ctx context.Context) (Connection, error) {
	obj := m.conn.Object(nmBusName, nmPath)

	var devicePaths []dbus.ObjectPath
	err := obj.Call(nmInterface+".GetDevices", 0).Store(&devicePaths)
	if err != nil {
		return Connection{}, fmt.Errorf("falha ao buscar dispositivos: %w", err)
	}

	for _, path := range devicePaths {
		devObj := m.conn.Object(nmBusName, path)

		var devType uint32
		if v, err := devObj.GetProperty("org.freedesktop.NetworkManager.Device.DeviceType"); err == nil {
			_ = v.Store(&devType)
			if devType != nmDeviceWifi {
				continue
			}
		} else {
			continue
		}

		var ifaceName string
		if v, err := devObj.GetProperty("org.freedesktop.NetworkManager.Device.Interface"); err == nil {
			_ = v.Store(&ifaceName)
		}

		var activeAPPath dbus.ObjectPath
		if v, err := devObj.GetProperty("org.freedesktop.NetworkManager.Device.Wireless.ActiveAccessPoint"); err == nil {
			_ = v.Store(&activeAPPath)
		}

		if activeAPPath == "/" || activeAPPath == "" {
			continue
		}

		apObj := m.conn.Object(nmBusName, activeAPPath)
		var ssidBytes []byte
		if v, err := apObj.GetProperty("org.freedesktop.NetworkManager.AccessPoint.Ssid"); err == nil {
			_ = v.Store(&ssidBytes) // Corrigido aqui para aceitar apenas 1 argumento
		}

		if len(ssidBytes) > 0 {
			return Connection{
				SSID:      string(ssidBytes),
				Interface: ifaceName,
				IPv4:      net.IP{},
			}, nil
		}
	}

	return Connection{}, fmt.Errorf("nenhuma rede Wi-Fi ativa encontrada")
}