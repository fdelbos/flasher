package esp

import "fmt"

// MACs holds the per-interface MAC addresses the ESP-IDF derives from the factory
// base MAC. This follows the default derivation for chips with two universal MAC
// addresses (ESP32-C6, C3, S3): the BT MAC is the base MAC + 1 in the last octet;
// the SoftAP/ETH MACs are locally-administered derivations (not exposed here).
//
// Espressif allocates universal MACs so the base MAC's last octet is even, so the
// +1 never carries.
type MACs struct {
	WiFiSTA [6]byte // = base MAC
	BT      [6]byte // Bluetooth / BLE
}

// DeriveMACs computes the interface MACs from a factory base MAC.
func DeriveMACs(base [6]byte) MACs {
	bt := base
	bt[5]++
	return MACs{WiFiSTA: base, BT: bt}
}

// FormatMAC renders a MAC as colon-separated lowercase hex.
func FormatMAC(m [6]byte) string {
	return fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x", m[0], m[1], m[2], m[3], m[4], m[5])
}

// HexID renders a MAC as lowercase hex with no separators — the form airlock
// stores as a device's hardware_serial.
func HexID(m [6]byte) string {
	return fmt.Sprintf("%02x%02x%02x%02x%02x%02x", m[0], m[1], m[2], m[3], m[4], m[5])
}

// MACs reads the base MAC over the bootloader and returns the derived interface MACs.
func (l *Loader) MACs() (MACs, error) {
	base, err := l.BaseMAC()
	if err != nil {
		return MACs{}, err
	}
	return DeriveMACs(base), nil
}
