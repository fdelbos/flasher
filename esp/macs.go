package esp

import "fmt"

// MACs holds the per-interface MAC addresses ESP-IDF derives from the factory
// base MAC. How many are "universal" (assigned by adding to the last octet)
// depends on the target:
//
//	4 universal (e.g. C6): STA=base+0  AP=base+1  BT/BLE=base+2  ETH=base+3
//	2 universal (e.g. S3): STA=base+0 and BT/BLE=base+1 are universal; AP and ETH
//	                       are locally administered (base[0] |= 0x02).
//
// The C6 BT offset (+2) was verified against the firmware's own BLE_INIT log; the
// S3 BT offset (+1) should be re-verified the same way once firmware is flashed.
type MACs struct {
	WiFiSTA [6]byte
	WiFiAP  [6]byte
	BT      [6]byte
	ETH     [6]byte
}

// DeriveMACs computes the interface MACs from a factory base MAC, following the
// target's universal-MAC scheme (see MACs). `local` mirrors esp_derive_local_mac.
func DeriveMACs(base [6]byte, universal int) MACs {
	add := func(n byte) [6]byte {
		m := base
		m[5] += n
		return m
	}
	local := func(u [6]byte) [6]byte {
		m := u
		m[0] |= 0x02 // set the locally-administered bit
		if m[0] == u[0] {
			m[0] ^= 0x04 // already local: flip a bit (matches esp_derive_local_mac)
		}
		return m
	}
	if universal >= 4 {
		return MACs{WiFiSTA: add(0), WiFiAP: add(1), BT: add(2), ETH: add(3)}
	}
	sta, bt := add(0), add(1)
	return MACs{WiFiSTA: sta, WiFiAP: local(sta), BT: bt, ETH: local(bt)}
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

// MACs reads the base MAC over the bootloader and returns the derived interface
// MACs, using the detected target's universal-MAC scheme.
func (l *Loader) MACs() (MACs, error) {
	if err := l.ensureChip(); err != nil {
		return MACs{}, err
	}
	base, err := l.BaseMAC()
	if err != nil {
		return MACs{}, err
	}
	return DeriveMACs(base, l.chip.UniversalMACs), nil
}
