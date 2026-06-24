package esp

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestSLIPEncodeEscapes(t *testing.T) {
	in := []byte{0x01, slipEnd, 0x02, slipEsc, 0x03}
	got := slipEncode(in)
	want := []byte{slipEnd, 0x01, slipEsc, slipEscEnd, 0x02, slipEsc, slipEscEsc, 0x03, slipEnd}
	if !bytes.Equal(got, want) {
		t.Fatalf("slipEncode = % x, want % x", got, want)
	}
}

func TestChecksum(t *testing.T) {
	if got := checksum(nil); got != uint32(checksumMagic) {
		t.Fatalf("empty checksum = %#x, want %#x", got, checksumMagic)
	}
	data := []byte{0x01, 0x02, 0x03}
	want := uint32(checksumMagic ^ 0x01 ^ 0x02 ^ 0x03)
	if got := checksum(data); got != want {
		t.Fatalf("checksum = %#x, want %#x", got, want)
	}
}

func TestEncodeCommand(t *testing.T) {
	pkt := encodeCommand(cmdReadReg, []byte{0xAA, 0xBB, 0xCC, 0xDD}, 0)
	if pkt[0] != 0x00 || command(pkt[1]) != cmdReadReg {
		t.Fatalf("header dir/cmd = %#x/%#x", pkt[0], pkt[1])
	}
	if got := binary.LittleEndian.Uint16(pkt[2:]); got != 4 {
		t.Fatalf("size = %d, want 4", got)
	}
}

func TestParseResponseAndStatus(t *testing.T) {
	// dir=1, cmd=READ_REG, size=4, value=0x12345678, data=ROM trailer {status=0,err=0,0,0}
	frame := make([]byte, 12)
	frame[0] = 0x01
	frame[1] = byte(cmdReadReg)
	binary.LittleEndian.PutUint16(frame[2:], 4)
	binary.LittleEndian.PutUint32(frame[4:], 0x12345678)
	resp, ok := parseResponse(frame)
	if !ok {
		t.Fatal("parseResponse failed")
	}
	if resp.value != 0x12345678 {
		t.Fatalf("value = %#x", resp.value)
	}
	if ok, _ := resp.status(false); !ok {
		t.Fatal("status should be success")
	}
}
