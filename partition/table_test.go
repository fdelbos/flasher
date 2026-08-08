package partition

import (
	"os"
	"testing"
)

// testdata/partition-table.bin is a real esp-idf build artifact (esp32c6, 8MB).
func TestParseTable(t *testing.T) {
	data, err := os.ReadFile("testdata/partition-table.bin")
	if err != nil {
		t.Fatal(err)
	}
	entries, err := ParseTable(data)
	if err != nil {
		t.Fatal(err)
	}

	want := []Entry{
		{Name: "nvs", Type: TypeData, SubType: 0x02, Offset: 0x11000, Size: 0x60000},
		{Name: "otadata", Type: TypeData, SubType: 0x00, Offset: 0x71000, Size: 0x2000},
		{Name: "phy_init", Type: TypeData, SubType: 0x01, Offset: 0x73000, Size: 0x1000},
		{Name: "ota_0", Type: TypeApp, SubType: 0x10, Offset: 0x80000, Size: 0x300000},
		{Name: "ota_1", Type: TypeApp, SubType: 0x11, Offset: 0x380000, Size: 0x300000},
		{Name: "config", Type: TypeData, SubType: 0x02, Offset: 0x680000, Size: 0x3000},
		{Name: "var", Type: TypeData, SubType: 0x02, Offset: 0x683000, Size: 0x3000},
	}
	if len(entries) < len(want) {
		t.Fatalf("got %d entries, want at least %d: %+v", len(entries), len(want), entries)
	}
	for i, w := range want {
		g := entries[i]
		if g.Name != w.Name || g.Type != w.Type || g.SubType != w.SubType ||
			g.Offset != w.Offset || g.Size != w.Size {
			t.Errorf("entry %d: got %+v, want %+v", i, g, w)
		}
	}

	if e := FindEntry(entries, "config"); e == nil || e.Offset != 0x680000 {
		t.Errorf("FindEntry(config) = %+v", e)
	}
	if e := FindEntry(entries, "nope"); e != nil {
		t.Errorf("FindEntry(nope) = %+v, want nil", e)
	}
}

func TestParseTableErrors(t *testing.T) {
	if _, err := ParseTable(make([]byte, 64)); err == nil {
		t.Error("all-zero table: want error")
	}
	empty := make([]byte, 64)
	for i := range empty {
		empty[i] = 0xFF
	}
	if _, err := ParseTable(empty); err == nil {
		t.Error("erased table: want error")
	}
}
