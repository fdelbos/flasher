package esp

import (
	"bytes"
	"compress/zlib"
	"crypto/md5"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

const flashBeginTimeout = 60 * time.Second

func put32(b []byte, v uint32) { binary.LittleEndian.PutUint32(b, v) }

// flashBlockSize is the write block size: 16 KiB under the stub, 1 KiB on the ROM.
func (l *Loader) flashBlockSize() uint32 {
	if l.stub {
		return 0x4000
	}
	return 0x400
}

// SpiAttach attaches the default SPI flash. Required before flashing in ROM mode;
// the stub attaches SPI itself, so this is a no-op under the stub.
func (l *Loader) SpiAttach() error {
	if l.stub {
		return nil
	}
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
	old := uint32(0) // ROM loader expects 0
	if l.stub {
		old = uint32(l.curBaud) // stub expects the current baud
	}
	d := make([]byte, 8)
	put32(d[0:], uint32(newBaud))
	put32(d[4:], old)
	if _, err := l.command(cmdChangeBaudrate, d, 0, cmdTimeout); err != nil {
		return err
	}
	if err := l.t.SetBaud(newBaud); err != nil {
		return err
	}
	l.curBaud = newBaud
	time.Sleep(50 * time.Millisecond)
	return l.t.FlushInput()
}

func (l *Loader) flashBegin(size, offset uint32) error {
	bs := l.flashBlockSize()
	numBlocks := (size + bs - 1) / bs
	// Chips that support encrypted flash (C6 etc.) want a 5th word — the
	// "begin encrypted" flag — on the ROM loader (omit it under the stub).
	n := 16
	if !l.stub {
		n = 20
	}
	d := make([]byte, n)
	put32(d[0:], size) // erase size
	put32(d[4:], numBlocks)
	put32(d[8:], bs)
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

// WriteFlash erases and programs data at offset, then verifies the region with the
// chip's own MD5. Under the stub it uses compressed transfer (far less data over
// the wire); on the ROM loader it writes raw blocks. progress(done,total) is
// called per block.
func (l *Loader) WriteFlash(offset uint32, data []byte, progress func(done, total int)) error {
	if l.stub {
		return l.writeFlashCompressed(offset, data, progress)
	}
	return l.writeFlashRaw(offset, data, progress)
}

func (l *Loader) writeFlashRaw(offset uint32, data []byte, progress func(done, total int)) error {
	bs := int(l.flashBlockSize())
	padded := data
	if rem := len(data) % bs; rem != 0 {
		padded = make([]byte, len(data)+bs-rem)
		copy(padded, data)
		for i := len(data); i < len(padded); i++ {
			padded[i] = 0xFF
		}
	}
	size := uint32(len(padded))
	if err := l.flashBegin(size, offset); err != nil {
		return fmt.Errorf("flash_begin @0x%x: %w", offset, err)
	}
	total := len(padded) / bs
	for seq := 0; seq < total; seq++ {
		block := padded[seq*bs : (seq+1)*bs]
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

// writeFlashCompressed deflates data and streams it via FLASH_DEFL_*. The stub
// inflates on the fly. The deflate stream is stateful, so blocks are not retried
// (a resend would corrupt the stream); writeAll guarantees whole frames anyway.
func (l *Loader) writeFlashCompressed(offset uint32, data []byte, progress func(done, total int)) error {
	var buf bytes.Buffer
	zw, _ := zlib.NewWriterLevel(&buf, zlib.BestCompression)
	if _, err := zw.Write(data); err != nil {
		return err
	}
	if err := zw.Close(); err != nil {
		return err
	}
	comp := buf.Bytes()
	bs := int(l.flashBlockSize())
	numBlocks := (len(comp) + bs - 1) / bs

	d := make([]byte, 16) // no 5th word under the stub
	put32(d[0:], uint32(len(data)))   // uncompressed size (region to erase/write)
	put32(d[4:], uint32(numBlocks))   // number of compressed blocks
	put32(d[8:], uint32(bs))
	put32(d[12:], offset)
	if _, err := l.command(cmdFlashDeflBegin, d, 0, flashBeginTimeout); err != nil {
		return fmt.Errorf("flash_defl_begin @0x%x: %w", offset, err)
	}
	for seq := 0; seq < numBlocks; seq++ {
		start := seq * bs
		end := start + bs
		if end > len(comp) {
			end = len(comp)
		}
		chunk := comp[start:end]
		b := make([]byte, 16+len(chunk))
		put32(b[0:], uint32(len(chunk)))
		put32(b[4:], uint32(seq))
		copy(b[16:], chunk)
		if _, err := l.command(cmdFlashDeflData, b, checksum(chunk), cmdTimeout); err != nil {
			return fmt.Errorf("flash_defl_data seq %d @0x%x: %w", seq, offset, err)
		}
		if progress != nil {
			progress(seq+1, numBlocks)
		}
	}
	want := md5.Sum(data)
	got, err := l.flashMD5(offset, uint32(len(data)))
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
