package esp

import "testing"

func TestDeriveMACs(t *testing.T) {
	base := [6]byte{0xfc, 0x01, 0x2c, 0xfe, 0x77, 0xbc}
	m := DeriveMACs(base)
	cases := map[string]string{
		"STA": HexID(m.WiFiSTA),
		"AP":  HexID(m.WiFiAP),
		"BT":  HexID(m.BT),
		"ETH": HexID(m.ETH),
	}
	want := map[string]string{
		"STA": "fc012cfe77bc",
		"AP":  "fc012cfe77bd",
		"BT":  "fc012cfe77be", // base+2, verified against firmware BLE_INIT
		"ETH": "fc012cfe77bf",
	}
	for k, v := range want {
		if cases[k] != v {
			t.Errorf("%s = %s, want %s", k, cases[k], v)
		}
	}
}

func TestEUI64Hex(t *testing.T) {
	base := [6]byte{0xfc, 0x01, 0x2c, 0xfe, 0x77, 0xbc}
	if got := EUI64Hex(base); got != "fc012cfffefe77bc" {
		t.Errorf("EUI64Hex = %s, want fc012cfffefe77bc", got)
	}
}
