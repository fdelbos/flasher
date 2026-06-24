package esp

import (
	"io"
	"time"

	"go.bug.st/serial"
)

// Transport is the byte channel to the chip. Abstracted so the loader can run
// over a real serial port, a network bridge, or a test double.
type Transport interface {
	io.ReadWriteCloser
	SetBaud(int) error
	SetDTR(bool) error
	SetRTS(bool) error
	SetReadTimeout(time.Duration) error
	FlushInput() error
}

// serialTransport adapts go.bug.st/serial to Transport.
type serialTransport struct {
	p serial.Port
}

// OpenSerial opens a serial port at the given baud rate. DTR/RTS are held
// deasserted on open so that merely attaching does not pulse the ESP auto-reset
// circuit and reboot the board (so `monitor` can watch a running device).
// Commands that need a reset (Connect, flashing) drive the lines explicitly after.
func OpenSerial(name string, baud int) (Transport, error) {
	p, err := serial.Open(name, &serial.Mode{
		BaudRate:          baud,
		InitialStatusBits: &serial.ModemOutputBits{RTS: false, DTR: false},
	})
	if err != nil {
		return nil, err
	}
	return &serialTransport{p: p}, nil
}

func (s *serialTransport) Read(b []byte) (int, error)  { return s.p.Read(b) }
func (s *serialTransport) Write(b []byte) (int, error) { return s.p.Write(b) }
func (s *serialTransport) Close() error                { return s.p.Close() }
func (s *serialTransport) SetBaud(b int) error         { return s.p.SetMode(&serial.Mode{BaudRate: b}) }
func (s *serialTransport) SetDTR(v bool) error         { return s.p.SetDTR(v) }
func (s *serialTransport) SetRTS(v bool) error         { return s.p.SetRTS(v) }
func (s *serialTransport) SetReadTimeout(d time.Duration) error {
	return s.p.SetReadTimeout(d)
}
func (s *serialTransport) FlushInput() error { return s.p.ResetInputBuffer() }
