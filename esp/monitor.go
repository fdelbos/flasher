package esp

import (
	"context"
	"io"
	"time"
)

// Monitor streams serial output from the port to w until ctx is cancelled. It is
// non-interactive (read-only); stdin is never forwarded. If reset is true it
// pulses the board to reboot into the application first, so boot logs are caught.
func Monitor(ctx context.Context, port string, baud int, w io.Writer, reset bool) error {
	t, err := OpenSerial(port, baud)
	if err != nil {
		return err
	}
	defer t.Close()

	if reset {
		t.SetDTR(false) // IO0 high: boot the app, not the download ROM
		t.SetRTS(true)  // EN low: into reset
		time.Sleep(100 * time.Millisecond)
		t.SetRTS(false) // EN high: release -> boot
	}

	buf := make([]byte, 1024)
	for {
		if ctx.Err() != nil {
			return nil
		}
		if err := t.SetReadTimeout(200 * time.Millisecond); err != nil {
			return err
		}
		n, err := t.Read(buf) // returns (0, nil) on timeout
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return werr
			}
		}
		if err != nil {
			return err
		}
	}
}
