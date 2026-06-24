package esp

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"time"
)

const (
	ROMBaud         = 115200
	syncTimeout     = 100 * time.Millisecond
	cmdTimeout      = 3 * time.Second
	connectAttempts = 7
)

// ErrTimeout is returned when a read does not complete within the deadline.
var ErrTimeout = errors.New("esp: read timeout")

// Loader speaks the ESP serial bootloader protocol over a Transport.
type Loader struct {
	t       Transport
	stub    bool // false = ROM loader (4-byte status trailer), true = stub (2-byte)
	curBaud int  // current port baud, tracked for CHANGE_BAUDRATE under the stub
}

// NewLoader wraps a Transport. Call Connect before issuing commands.
func NewLoader(t Transport) *Loader { return &Loader{t: t, curBaud: ROMBaud} }

// Close closes the underlying transport.
func (l *Loader) Close() error { return l.t.Close() }

// --- SLIP framing ---

func (l *Loader) readByte() (byte, error) {
	var b [1]byte
	n, err := l.t.Read(b[:])
	if err != nil {
		return 0, err
	}
	if n == 0 {
		return 0, ErrTimeout
	}
	return b[0], nil
}

func (l *Loader) readFrame(timeout time.Duration) ([]byte, error) {
	if err := l.t.SetReadTimeout(timeout); err != nil {
		return nil, err
	}
	for { // seek frame start
		b, err := l.readByte()
		if err != nil {
			return nil, err
		}
		if b == slipEnd {
			break
		}
	}
	out := make([]byte, 0, 64)
	for {
		b, err := l.readByte()
		if err != nil {
			return nil, err
		}
		switch b {
		case slipEnd:
			if len(out) == 0 {
				continue // collapse doubled delimiters
			}
			return out, nil
		case slipEsc:
			n, err := l.readByte()
			if err != nil {
				return nil, err
			}
			switch n {
			case slipEscEnd:
				out = append(out, slipEnd)
			case slipEscEsc:
				out = append(out, slipEsc)
			default:
				return nil, fmt.Errorf("esp: bad SLIP escape 0x%02x", n)
			}
		default:
			out = append(out, b)
		}
	}
}

// writeAll writes every byte, looping over partial writes (a full serial output
// buffer otherwise truncates a frame and the chip rejects the packet).
func (l *Loader) writeAll(b []byte) error {
	for len(b) > 0 {
		n, err := l.t.Write(b)
		if err != nil {
			return err
		}
		if n == 0 {
			return errors.New("esp: serial short write")
		}
		b = b[n:]
	}
	return nil
}

// command sends a request and returns the matching, status-checked response.
func (l *Loader) command(cmd command, data []byte, chk uint32, timeout time.Duration) (*response, error) {
	if err := l.writeAll(slipEncode(encodeCommand(cmd, data, chk))); err != nil {
		return nil, err
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		frame, err := l.readFrame(timeout)
		if err != nil {
			return nil, err
		}
		resp, ok := parseResponse(frame)
		if !ok || resp.cmd != cmd {
			continue
		}
		if ok, code := resp.status(l.stub); !ok {
			return resp, &cmdError{cmd: cmd, code: code}
		}
		return resp, nil
	}
	return nil, ErrTimeout
}

// --- reset & connect ---

// resetToBootloader runs the classic auto-reset (DTR->IO0, RTS->EN) used by
// USB-UART bridges (CP210x/CH34x). Native USB-Serial-JTAG needs a different seq.
func (l *Loader) resetToBootloader() {
	l.t.SetDTR(false) // IO0 high
	l.t.SetRTS(true)  // EN low: hold in reset
	time.Sleep(100 * time.Millisecond)
	l.t.SetDTR(true)  // IO0 low: select download mode
	l.t.SetRTS(false) // EN high: release reset
	time.Sleep(50 * time.Millisecond)
	l.t.SetDTR(false) // release IO0
	l.t.FlushInput()
}

// HardReset pulses EN to reboot into the application firmware.
func (l *Loader) HardReset() {
	l.t.SetRTS(true)
	time.Sleep(100 * time.Millisecond)
	l.t.SetRTS(false)
}

var syncPayload = func() []byte {
	p := []byte{0x07, 0x07, 0x12, 0x20}
	for i := 0; i < 32; i++ {
		p = append(p, 0x55)
	}
	return p
}()

func (l *Loader) sync() error {
	if _, err := l.command(cmdSync, syncPayload, 0, syncTimeout); err != nil {
		return err
	}
	for i := 0; i < 7; i++ { // drain the chip's extra sync echoes
		if _, err := l.readFrame(syncTimeout); err != nil {
			break
		}
	}
	return nil
}

// Connect resets the chip into the ROM bootloader and establishes SYNC.
func (l *Loader) Connect(ctx context.Context) error {
	for attempt := 0; attempt < connectAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		l.resetToBootloader()
		for i := 0; i < 5; i++ {
			if err := l.sync(); err == nil {
				return nil
			}
		}
	}
	return errors.New("esp: failed to sync with chip (check wiring / hold BOOT)")
}

// --- registers / identity ---

// ReadReg reads a 32-bit value from a chip register.
func (l *Loader) ReadReg(addr uint32) (uint32, error) {
	data := make([]byte, 4)
	binary.LittleEndian.PutUint32(data, addr)
	resp, err := l.command(cmdReadReg, data, 0, cmdTimeout)
	if err != nil {
		return 0, err
	}
	return resp.value, nil
}

// BaseMAC reads the 6-byte factory base MAC from eFuse (C6 layout).
func (l *Loader) BaseMAC() ([6]byte, error) {
	var mac [6]byte
	mac0, err := l.ReadReg(c6MacReg)
	if err != nil {
		return mac, err
	}
	mac1, err := l.ReadReg(c6MacReg + 4)
	if err != nil {
		return mac, err
	}
	var buf [8]byte
	binary.BigEndian.PutUint32(buf[0:], mac1)
	binary.BigEndian.PutUint32(buf[4:], mac0)
	copy(mac[:], buf[2:]) // base MAC is the low 6 bytes
	return mac, nil
}

// ChipID returns the chip id from GET_SECURITY_INFO (best-effort; not all ROM
// revisions include it).
func (l *Loader) ChipID() (uint32, error) {
	resp, err := l.command(cmdGetSecurityInfo, nil, 0, cmdTimeout)
	if err != nil {
		return 0, err
	}
	// layout: flags(4) flash_crypt_cnt(1) key_purposes(7) chip_id(4) api_version(4)
	p := resp.payload(l.stub)
	if len(p) < 16 {
		return 0, fmt.Errorf("esp: security info too short (%d bytes)", len(p))
	}
	return binary.LittleEndian.Uint32(p[12:16]), nil
}
