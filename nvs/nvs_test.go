package nvs

import (
	"bytes"
	"encoding/hex"
	"strings"
	"testing"
)

func hexb(s string) []byte {
	b, err := hex.DecodeString(strings.ReplaceAll(s, " ", ""))
	if err != nil {
		panic(err)
	}
	return b
}

// TestWorkedExample checks the generated image byte-for-byte against the output
// of the official nvs_partition_gen.py for the same input (see FORMAT.md §9).
func TestWorkedExample(t *testing.T) {
	p, err := New(0x3000)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.SetU8("storage", "x", 42); err != nil {
		t.Fatal(err)
	}
	if err := p.SetString("storage", "name", "hi"); err != nil {
		t.Fatal(err)
	}
	out, err := p.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 0x3000 {
		t.Fatalf("size = %d, want 0x3000", len(out))
	}

	var want []byte
	want = append(want, hexb("fe ff ff ff 00 00 00 00 fe ff ff ff ff ff ff ff ff ff ff ff ff ff ff ff ff ff ff ff 84 2d ba b9")...) // header
	want = append(want, 0xaa)                                  // bitmap byte 0
	want = append(want, bytes.Repeat([]byte{0xff}, 31)...)     // bitmap rest
	want = append(want, hexb("00 01 01 ff 09 a9 50 07 73 74 6f 72 61 67 65 00 00 00 00 00 00 00 00 00 01 ff ff ff ff ff ff ff")...) // ns "storage"
	want = append(want, hexb("01 01 01 ff 92 2c 49 14 78 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 2a ff ff ff ff ff ff ff")...) // x = u8 42
	want = append(want, hexb("01 21 02 ff 44 a5 41 b4 6e 61 6d 65 00 00 00 00 00 00 00 00 00 00 00 00 03 00 ff ff 89 b5 04 0d")...) // name head
	want = append(want, hexb("68 69 00")...)                   // "hi\0"
	want = append(want, bytes.Repeat([]byte{0xff}, 29)...)     // data entry padding

	if !bytes.Equal(out[:192], want) {
		for i := 0; i < 192; i++ {
			if out[i] != want[i] {
				t.Fatalf("byte %d (0x%x): got %02x, want %02x", i, i, out[i], want[i])
			}
		}
	}
	for i := 192; i < len(out); i++ {
		if out[i] != 0xFF {
			t.Fatalf("byte 0x%x should be 0xFF, got %02x", i, out[i])
		}
	}
}

func TestKeyTooLong(t *testing.T) {
	p, _ := New(0x3000)
	if err := p.SetU8("ns", "this_key_is_too_long", 1); err == nil {
		t.Fatal("expected error for >15-char key")
	}
}
