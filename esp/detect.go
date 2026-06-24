package esp

import (
	"fmt"
	"strings"

	"go.bug.st/serial/enumerator"
)

// knownVIDs are USB vendor IDs commonly used by ESP dev boards and USB-UART
// bridges. Ports matching these are preferred during auto-detection.
var knownVIDs = map[string]string{
	"10c4": "Silicon Labs CP210x",
	"1a86": "WCH CH34x",
	"0403": "FTDI",
	"303a": "Espressif",
}

// PortInfo describes a serial port discovered on the host.
type PortInfo struct {
	Name         string
	IsUSB        bool
	VID, PID     string
	Product      string
	SerialNumber string
	Vendor       string // known-vendor label, "" if unrecognized
}

// ListPorts returns all serial ports on the host, with USB details where available.
func ListPorts() ([]PortInfo, error) {
	ports, err := enumerator.GetDetailedPortsList()
	if err != nil {
		return nil, err
	}
	out := make([]PortInfo, 0, len(ports))
	for _, p := range ports {
		pi := PortInfo{Name: p.Name, IsUSB: p.IsUSB}
		if p.IsUSB {
			pi.VID, pi.PID = p.VID, p.PID
			pi.Product, pi.SerialNumber = p.Product, p.SerialNumber
			pi.Vendor = knownVIDs[strings.ToLower(p.VID)]
		}
		out = append(out, pi)
	}
	return out, nil
}

// DetectPort picks the serial port to use when none is given. It considers only
// USB serial ports, preferring those with a known ESP bridge/USB vendor id. It
// returns an error (listing candidates) if it can't unambiguously choose one.
func DetectPort() (string, error) {
	ports, err := ListPorts()
	if err != nil {
		return "", err
	}
	var preferred, other []string
	for _, p := range ports {
		if !p.IsUSB {
			continue
		}
		if p.Vendor != "" {
			preferred = append(preferred, p.Name)
		} else {
			other = append(other, p.Name)
		}
	}
	switch {
	case len(preferred) == 1:
		return preferred[0], nil
	case len(preferred) > 1:
		return "", fmt.Errorf("multiple ESP-like serial ports found %v; pass one explicitly", preferred)
	case len(other) == 1:
		return other[0], nil
	case len(other) == 0:
		return "", fmt.Errorf("no USB serial ports found; pass <port> explicitly")
	default:
		return "", fmt.Errorf("multiple serial ports found %v; pass one explicitly", other)
	}
}
