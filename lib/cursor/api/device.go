package cursor_api_sdk

import (
	"crypto/sha256"
	"encoding/hex"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
)

// DeviceIDs are stable fingerprints embedded in x-cursor-checksum.
type DeviceIDs struct {
	MachineID    string
	MacMachineID string // empty if no MAC
}

var (
	deviceOnce sync.Once
	deviceIDs  DeviceIDs
)

// GetDeviceIDs returns process-cached stable device fingerprints.
func GetDeviceIDs() DeviceIDs {
	deviceOnce.Do(func() {
		deviceIDs = deriveDeviceIDs()
	})
	return deviceIDs
}

func deriveDeviceIDs() DeviceIDs {
	mac := firstMACAddress()
	uuid := platformUUID()

	var machineID string
	switch {
	case uuid != "":
		machineID = sha256Hex(uuid)
	case mac != "":
		machineID = sha256Hex(mac)
	default:
		host, _ := os.Hostname()
		machineID = sha256Hex(host)
	}

	macMachineID := ""
	if mac != "" {
		macMachineID = sha256Hex(mac)
	}
	return DeviceIDs{MachineID: machineID, MacMachineID: macMachineID}
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func platformUUID() string {
	switch runtime.GOOS {
	case "linux":
		for _, p := range []string{"/etc/machine-id", "/var/lib/dbus/machine-id"} {
			data, err := os.ReadFile(p)
			if err != nil {
				continue
			}
			if v := strings.TrimSpace(string(data)); v != "" {
				return v
			}
		}
	case "darwin":
		out, err := exec.Command("ioreg", "-rd1", "-c", "IOPlatformExpertDevice").Output()
		if err != nil {
			return ""
		}
		s := string(out)
		i := strings.Index(s, "IOPlatformUUID")
		if i < 0 {
			return ""
		}
		line := s[i:]
		if nl := strings.IndexByte(line, '\n'); nl >= 0 {
			line = line[:nl]
		}
		line = strings.ToLower(line)
		replacer := strings.NewReplacer("=", "", "\"", "", " ", "", "\t", "")
		line = replacer.Replace(line)
		line = strings.TrimPrefix(line, "ioplatformuuid")
		return strings.TrimSpace(line)
	case "windows":
		out, err := exec.Command("reg", "query", `HKLM\SOFTWARE\Microsoft\Cryptography`, "/v", "MachineGuid").Output()
		if err != nil {
			return ""
		}
		s := string(out)
		i := strings.Index(s, "MachineGuid")
		if i < 0 {
			return ""
		}
		rest := s[i+len("MachineGuid"):]
		fields := strings.Fields(rest)
		if len(fields) == 0 {
			return ""
		}
		return fields[len(fields)-1]
	}
	return ""
}

func firstMACAddress() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, iface := range ifaces {
		hw := iface.HardwareAddr.String()
		if hw == "" || hw == "00:00:00:00:00:00" {
			continue
		}
		return hw
	}
	return ""
}
