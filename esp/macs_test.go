package esp

import "testing"

func TestDeriveMACs4(t *testing.T) { // 4 universal MACs (e.g. C6)
	base := [6]byte{0xfc, 0x01, 0x2c, 0xfe, 0x77, 0xbc}
	m := DeriveMACs(base, 4)
	want := map[string]string{
		"STA": "fc012cfe77bc",
		"AP":  "fc012cfe77bd",
		"BT":  "fc012cfe77be", // base+2, verified against firmware BLE_INIT
		"ETH": "fc012cfe77bf",
	}
	got := map[string]string{"STA": HexID(m.WiFiSTA), "AP": HexID(m.WiFiAP), "BT": HexID(m.BT), "ETH": HexID(m.ETH)}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %s, want %s", k, got[k], v)
		}
	}
}

func TestDeriveMACs2(t *testing.T) { // 2 universal MACs (e.g. S3)
	base := [6]byte{0x68, 0xee, 0x8f, 0x52, 0x9a, 0xc8}
	m := DeriveMACs(base, 2)
	// STA=base, BT=base+1 (universal); AP=STA with the U/L bit set (0x68|0x02).
	want := map[string]string{
		"STA": "68ee8f529ac8",
		"BT":  "68ee8f529ac9",
		"AP":  "6aee8f529ac8",
	}
	got := map[string]string{"STA": HexID(m.WiFiSTA), "BT": HexID(m.BT), "AP": HexID(m.WiFiAP)}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %s, want %s", k, got[k], v)
		}
	}
}

func TestEUI64Hex(t *testing.T) {
	base := [6]byte{0xfc, 0x01, 0x2c, 0xfe, 0x77, 0xbc}
	if got := EUI64Hex(base); got != "fc012cfffefe77bc" {
		t.Errorf("EUI64Hex = %s, want fc012cfffefe77bc", got)
	}
}
