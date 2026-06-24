package esp

import "fmt"

// MACs holds the per-interface MAC addresses the ESP-IDF derives from the factory
// base MAC. The ESP32-C6's base MAC is 4-aligned and four universal MACs are
// assigned by adding to the last octet:
//
//	WiFi STA = base+0   WiFi AP = base+1   BT/BLE = base+2   ETH = base+3
//
// The BT offset (+2) was verified against the firmware's own BLE_INIT log.
type MACs struct {
	WiFiSTA [6]byte
	WiFiAP  [6]byte
	BT      [6]byte
	ETH     [6]byte
}

// DeriveMACs computes the interface MACs from a factory base MAC.
func DeriveMACs(base [6]byte) MACs {
	mk := func(add byte) [6]byte {
		m := base
		m[5] += add
		return m
	}
	return MACs{WiFiSTA: mk(0), WiFiAP: mk(1), BT: mk(2), ETH: mk(3)}
}

// FormatMAC renders a MAC as colon-separated lowercase hex.
func FormatMAC(m [6]byte) string {
	return fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x", m[0], m[1], m[2], m[3], m[4], m[5])
}

// HexID renders a 6-byte MAC as lowercase hex, no separators (e.g. fc012cfe77bc).
func HexID(m [6]byte) string {
	return fmt.Sprintf("%02x%02x%02x%02x%02x%02x", m[0], m[1], m[2], m[3], m[4], m[5])
}

// EUI64Hex renders the EUI-64 of a 6-byte MAC: ff fe inserted between the OUI and
// NIC halves, lowercase hex, no separators (e.g. fc012cfffefe77bc). This matches
// the 8-byte form some ESP tooling emits.
func EUI64Hex(m [6]byte) string {
	return fmt.Sprintf("%02x%02x%02xfffe%02x%02x%02x", m[0], m[1], m[2], m[3], m[4], m[5])
}

// MACs reads the base MAC over the bootloader and returns the derived interface MACs.
func (l *Loader) MACs() (MACs, error) {
	base, err := l.BaseMAC()
	if err != nil {
		return MACs{}, err
	}
	return DeriveMACs(base), nil
}
