package esp

import (
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"
)

// esp32c6.json is the prebuilt flasher stub from Espressif's esp-flasher-stub
// (Apache-2.0 / MIT). It is uploaded into chip RAM and run to speed up flashing.
//
//go:embed stubs/esp32c6.json
var stubC6JSON []byte

const espRAMBlock = 0x1800 // max MEM_DATA block (6 KiB)

type stubImage struct {
	Entry     uint32 `json:"entry"`
	Text      string `json:"text"` // base64
	TextStart uint32 `json:"text_start"`
	Data      string `json:"data"` // base64
	DataStart uint32 `json:"data_start"`
	BSSStart  uint32 `json:"bss_start"`
}

// RunStub uploads the flasher stub into RAM and starts it, switching the loader to
// stub mode (16 KiB flash blocks, 2-byte status trailer). No-op if already on the
// stub. On any failure the loader stays in ROM mode and the error is returned so
// the caller can fall back.
func (l *Loader) RunStub() error {
	if l.stub {
		return nil
	}
	var s stubImage
	if err := json.Unmarshal(stubC6JSON, &s); err != nil {
		return err
	}
	text, err := base64.StdEncoding.DecodeString(s.Text)
	if err != nil {
		return fmt.Errorf("decode stub text: %w", err)
	}
	data, err := base64.StdEncoding.DecodeString(s.Data)
	if err != nil {
		return fmt.Errorf("decode stub data: %w", err)
	}
	if err := l.memUpload(text, s.TextStart); err != nil {
		return fmt.Errorf("upload stub text: %w", err)
	}
	if err := l.memUpload(data, s.DataStart); err != nil {
		return fmt.Errorf("upload stub data: %w", err)
	}
	l.memFinish(s.Entry)
	// The running stub greets with "OHAI". The MEM_END response may arrive first;
	// scan a few frames for the greeting.
	for i := 0; i < 3; i++ {
		frame, err := l.readFrame(2 * time.Second)
		if err != nil {
			return fmt.Errorf("waiting for stub OHAI: %w", err)
		}
		if string(frame) == "OHAI" {
			l.stub = true
			return nil
		}
	}
	return fmt.Errorf("stub did not greet with OHAI")
}

func (l *Loader) memUpload(data []byte, addr uint32) error {
	n := uint32(len(data))
	numBlocks := (n + espRAMBlock - 1) / espRAMBlock
	d := make([]byte, 16)
	put32(d[0:], n)
	put32(d[4:], numBlocks)
	put32(d[8:], espRAMBlock)
	put32(d[12:], addr)
	if _, err := l.command(cmdMemBegin, d, 0, cmdTimeout); err != nil {
		return err
	}
	for seq := uint32(0); seq < numBlocks; seq++ {
		start := seq * espRAMBlock
		end := start + espRAMBlock
		if end > n {
			end = n
		}
		block := data[start:end]
		b := make([]byte, 16+len(block))
		put32(b[0:], uint32(len(block)))
		put32(b[4:], seq)
		copy(b[16:], block)
		if _, err := l.command(cmdMemData, b, checksum(block), cmdTimeout); err != nil {
			return err
		}
	}
	return nil
}

func (l *Loader) memFinish(entry uint32) {
	d := make([]byte, 8)
	if entry == 0 {
		put32(d[0:], 1) // "no entry" flag
	}
	put32(d[4:], entry)
	// The chip jumps to the stub immediately, so its MEM_END response is often
	// lost. Short timeout, ignore the result.
	_, _ = l.command(cmdMemEnd, d, 0, 200*time.Millisecond)
}
