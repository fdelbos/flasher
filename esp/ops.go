package esp

import (
	"bytes"
	"crypto/md5"
	"fmt"
	"time"
)

const (
	chipEraseTimeout = 120 * time.Second
	readBlockSize    = 0x1000
	readMaxInFlight  = 64
)

// EraseFlash erases the entire flash chip. Requires the stub loader.
func (l *Loader) EraseFlash() error {
	if !l.stub {
		return fmt.Errorf("esp: erase requires the stub loader")
	}
	_, err := l.command(cmdEraseFlash, nil, 0, chipEraseTimeout)
	return err
}

// EraseRegion erases a flash region; offset and size must be 4 KiB-aligned.
// Requires the stub loader.
func (l *Loader) EraseRegion(offset, size uint32) error {
	if !l.stub {
		return fmt.Errorf("esp: erase requires the stub loader")
	}
	d := make([]byte, 8)
	put32(d[0:], offset)
	put32(d[4:], size)
	_, err := l.command(cmdEraseRegion, d, 0, chipEraseTimeout)
	return err
}

// ReadFlash reads length bytes from offset, verifying the chip's MD5 of the data.
// Requires the stub loader. The stub streams data packets which we flow-control by
// acknowledging the running byte total after each.
func (l *Loader) ReadFlash(offset, length uint32, progress func(done, total int)) ([]byte, error) {
	if !l.stub {
		return nil, fmt.Errorf("esp: read requires the stub loader")
	}
	d := make([]byte, 16)
	put32(d[0:], offset)
	put32(d[4:], length)
	put32(d[8:], readBlockSize)
	put32(d[12:], readMaxInFlight)
	if _, err := l.command(cmdReadFlash, d, 0, cmdTimeout); err != nil {
		return nil, err
	}
	out := make([]byte, 0, length)
	for uint32(len(out)) < length {
		frame, err := l.readFrame(cmdTimeout)
		if err != nil {
			return nil, err
		}
		out = append(out, frame...)
		ack := make([]byte, 4)
		put32(ack, uint32(len(out)))
		if err := l.writeAll(slipEncode(ack)); err != nil {
			return nil, err
		}
		if progress != nil {
			progress(len(out), int(length))
		}
	}
	// final frame: 16-byte MD5 of the data
	if digest, err := l.readFrame(cmdTimeout); err == nil && len(digest) == 16 {
		want := md5.Sum(out)
		if !bytes.Equal(digest, want[:]) {
			return out, fmt.Errorf("esp: read md5 mismatch")
		}
	}
	return out, nil
}
