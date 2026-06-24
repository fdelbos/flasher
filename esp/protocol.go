package esp

import (
	"encoding/binary"
	"fmt"
)

// command is an ESP bootloader command opcode.
type command byte

const (
	cmdFlashBegin      command = 0x02
	cmdFlashData       command = 0x03
	cmdFlashEnd        command = 0x04
	cmdMemBegin        command = 0x05
	cmdMemEnd          command = 0x06
	cmdMemData         command = 0x07
	cmdSync            command = 0x08
	cmdWriteReg        command = 0x09
	cmdReadReg         command = 0x0A
	cmdSpiSetParams    command = 0x0B
	cmdSpiAttach       command = 0x0D
	cmdChangeBaudrate  command = 0x0F
	cmdFlashDeflBegin  command = 0x10
	cmdFlashDeflData   command = 0x11
	cmdFlashDeflEnd    command = 0x12
	cmdSpiFlashMD5     command = 0x13
	cmdGetSecurityInfo command = 0x14
	// stub-loader only
	cmdEraseFlash  command = 0xD0
	cmdEraseRegion command = 0xD1
	cmdReadFlash   command = 0xD2
	cmdRunUserCode command = 0xD3
)

const checksumMagic byte = 0xEF

// checksum is the ESP data checksum: 0xEF XOR-ed with every payload byte, widened
// to a little-endian uint32. Only meaningful for the *_DATA commands.
func checksum(data []byte) uint32 {
	c := checksumMagic
	for _, b := range data {
		c ^= b
	}
	return uint32(c)
}

// encodeCommand builds the 8-byte header + data for a request packet (pre-SLIP).
func encodeCommand(cmd command, data []byte, chk uint32) []byte {
	pkt := make([]byte, 8+len(data))
	pkt[0] = 0x00 // direction: request
	pkt[1] = byte(cmd)
	binary.LittleEndian.PutUint16(pkt[2:], uint16(len(data)))
	binary.LittleEndian.PutUint32(pkt[4:], chk)
	copy(pkt[8:], data)
	return pkt
}

// response is a parsed reply packet (post-SLIP-decode).
type response struct {
	cmd   command
	value uint32 // bytes 4-7: READ_REG result, else 0
	data  []byte // bytes 8+, including the status trailer
}

func parseResponse(frame []byte) (*response, bool) {
	if len(frame) < 8 || frame[0] != 0x01 {
		return nil, false
	}
	size := int(binary.LittleEndian.Uint16(frame[2:]))
	if len(frame) < 8+size {
		return nil, false
	}
	return &response{
		cmd:   command(frame[1]),
		value: binary.LittleEndian.Uint32(frame[4:]),
		data:  frame[8 : 8+size],
	}, true
}

// status returns the (status, error) trailer bytes. The ROM loader uses a 4-byte
// trailer; the stub loader uses 2. status==0 means success.
func (r *response) status(stub bool) (ok bool, errCode byte) {
	n := 4
	if stub {
		n = 2
	}
	if len(r.data) < n {
		return false, 0xFF
	}
	tail := r.data[len(r.data)-n:]
	return tail[0] == 0, tail[1]
}

// payload returns the response data with the status trailer stripped.
func (r *response) payload(stub bool) []byte {
	n := 4
	if stub {
		n = 2
	}
	if len(r.data) < n {
		return nil
	}
	return r.data[:len(r.data)-n]
}

type cmdError struct {
	cmd  command
	code byte
}

func (e *cmdError) Error() string {
	return fmt.Sprintf("esp: command 0x%02x failed with error 0x%02x", byte(e.cmd), e.code)
}
