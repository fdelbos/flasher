package esp

import (
	"crypto/md5"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

const (
	romFlashBlockSize = 0x400 // ROM loader write block size (1 KiB)
	flashBeginTimeout = 60 * time.Second
)

func put32(b []byte, v uint32) { binary.LittleEndian.PutUint32(b, v) }

// SpiAttach attaches the default SPI flash. Required before flashing in ROM mode.
func (l *Loader) SpiAttach() error {
	_, err := l.command(cmdSpiAttach, make([]byte, 8), 0, cmdTimeout) // [pins=0][0]
	return err
}

// SpiSetParams configures the SPI flash geometry (so erase/MD5 cover the chip).
func (l *Loader) SpiSetParams(flashSize uint32) error {
	d := make([]byte, 24)
	put32(d[0:], 0)        // flash id
	put32(d[4:], flashSize) // total size
	put32(d[8:], 0x10000)  // block size 64 KiB
	put32(d[12:], 0x1000)  // sector size 4 KiB
	put32(d[16:], 0x100)   // page size 256 B
	put32(d[20:], 0xffff)  // status mask
	_, err := l.command(cmdSpiSetParams, d, 0, cmdTimeout)
	return err
}

// ChangeBaud switches the chip and host to a new baud rate.
func (l *Loader) ChangeBaud(newBaud int) error {
	d := make([]byte, 8)
	put32(d[0:], uint32(newBaud))
	put32(d[4:], 0) // old baud = 0 for the ROM loader
	if _, err := l.command(cmdChangeBaudrate, d, 0, cmdTimeout); err != nil {
		return err
	}
	if err := l.t.SetBaud(newBaud); err != nil {
		return err
	}
	time.Sleep(50 * time.Millisecond)
	return l.t.FlushInput()
}

func (l *Loader) flashBegin(size, offset uint32) error {
	numBlocks := (size + romFlashBlockSize - 1) / romFlashBlockSize
	// Chips that support encrypted flash (C6 etc.) want a 5th word — the
	// "begin encrypted" flag — on the ROM loader (omit it under the stub).
	n := 16
	if !l.stub {
		n = 20
	}
	d := make([]byte, n)
	put32(d[0:], size) // erase size
	put32(d[4:], numBlocks)
	put32(d[8:], romFlashBlockSize)
	put32(d[12:], offset)
	// d[16:] (encrypted flag) stays 0
	_, err := l.command(cmdFlashBegin, d, 0, flashBeginTimeout)
	return err
}

func (l *Loader) flashBlock(seq uint32, block []byte) error {
	d := make([]byte, 16+len(block))
	put32(d[0:], uint32(len(block)))
	put32(d[4:], seq)
	copy(d[16:], block)
	_, err := l.command(cmdFlashData, d, checksum(block), cmdTimeout)
	return err
}

func (l *Loader) flashMD5(offset, size uint32) (string, error) {
	d := make([]byte, 16)
	put32(d[0:], offset)
	put32(d[4:], size)
	// Hashing a large region on-chip takes seconds; allow ample time.
	resp, err := l.command(cmdSpiFlashMD5, d, 0, flashBeginTimeout)
	if err != nil {
		return "", err
	}
	p := resp.payload(l.stub)
	switch {
	case len(p) >= 32: // ROM: 32 ASCII hex chars
		return strings.ToLower(string(p[:32])), nil
	case len(p) >= 16: // stub: 16 raw bytes
		return hex.EncodeToString(p[:16]), nil
	default:
		return "", fmt.Errorf("esp: short md5 response (%d bytes)", len(p))
	}
}

// WriteFlash erases and programs data at offset in ROM-mode blocks, then verifies
// the region with the chip's own MD5. progress(done,total) is called per block.
func (l *Loader) WriteFlash(offset uint32, data []byte, progress func(done, total int)) error {
	padded := data
	if rem := len(data) % romFlashBlockSize; rem != 0 {
		padded = make([]byte, len(data)+romFlashBlockSize-rem)
		copy(padded, data)
		for i := len(data); i < len(padded); i++ {
			padded[i] = 0xFF
		}
	}
	size := uint32(len(padded))
	if err := l.flashBegin(size, offset); err != nil {
		return fmt.Errorf("flash_begin @0x%x: %w", offset, err)
	}
	total := len(padded) / romFlashBlockSize
	for seq := 0; seq < total; seq++ {
		block := padded[seq*romFlashBlockSize : (seq+1)*romFlashBlockSize]
		// Re-send the same seq on a transient hiccup: FLASH_DATA addresses the
		// block by index (offset + seq*blocksize), so a retry is idempotent.
		var err error
		for attempt := 0; attempt < 3; attempt++ {
			if err = l.flashBlock(uint32(seq), block); err == nil {
				break
			}
			l.t.FlushInput()
			time.Sleep(10 * time.Millisecond)
		}
		if err != nil {
			return fmt.Errorf("flash_data seq %d @0x%x: %w", seq, offset, err)
		}
		if progress != nil {
			progress(seq+1, total)
		}
	}
	want := md5.Sum(padded)
	got, err := l.flashMD5(offset, size)
	if err != nil {
		return fmt.Errorf("md5 @0x%x: %w", offset, err)
	}
	if got != hex.EncodeToString(want[:]) {
		return fmt.Errorf("md5 mismatch @0x%x: chip=%s host=%s", offset, got, hex.EncodeToString(want[:]))
	}
	return nil
}

// FlashFinish ends the flash session. reboot=false leaves the chip in the loader.
func (l *Loader) FlashFinish(reboot bool) error {
	d := make([]byte, 4)
	if !reboot {
		put32(d, 1) // 1 = stay in loader, 0 = reboot
	}
	_, err := l.command(cmdFlashEnd, d, 0, cmdTimeout)
	return err
}
