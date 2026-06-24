// Package nvs generates ESP-IDF NVS partition images in pure Go — a replacement
// for nvs_partition_gen.py. See FORMAT.md for the byte-exact spec it implements.
package nvs

import (
	"encoding/binary"
	"fmt"
	"hash/crc32"
)

const (
	pageSize   = 4096
	entrySize  = 32
	entryCount = 126
	headerSize = 32
	bitmapSize = 32
	versionV2  = 0xFE

	stateActive = 0xFFFFFFFE
	stateFull   = 0xFFFFFFFC

	nsTableIndex = 0    // namespace entries live under namespace index 0
	chunkAny     = 0xFF // chunk_index for non-chunked items

	typeU8  = 0x01
	typeI8  = 0x11
	typeU16 = 0x02
	typeI16 = 0x12
	typeU32 = 0x04
	typeI32 = 0x14
	typeU64 = 0x08
	typeI64 = 0x18
	typeStr = 0x21
	typeBlob = 0x41
)

// nvsCRC32 is CRC-32/IEEE seeded with 0xFFFFFFFF (matches esp_rom_crc32_le and the
// generator's zlib.crc32(data, 0xFFFFFFFF)). NOT crc32.ChecksumIEEE.
func nvsCRC32(data []byte) uint32 { return crc32.Update(0xffffffff, crc32.IEEETable, data) }

type page struct {
	buf [pageSize]byte
	seq uint32
	n   int // next free entry index
}

func newPage(seq uint32) *page {
	p := &page{seq: seq}
	for i := range p.buf {
		p.buf[i] = 0xFF
	}
	return p
}

func (pg *page) free() int { return entryCount - pg.n }

// writeEntry copies a 32-byte entry into the next slot and marks it WRITTEN.
func (pg *page) writeEntry(e []byte) {
	off := headerSize + bitmapSize + pg.n*entrySize
	copy(pg.buf[off:off+entrySize], e)
	bit := pg.n * 2 // EMPTY(11) -> WRITTEN(10): clear the low bit of the pair
	pg.buf[headerSize+bit/8] &^= 1 << (uint(bit) & 7)
	pg.n++
}

func (pg *page) finalize(state uint32) {
	binary.LittleEndian.PutUint32(pg.buf[0:], state)
	binary.LittleEndian.PutUint32(pg.buf[4:], pg.seq)
	pg.buf[8] = versionV2
	// buf[9:28] stay 0xFF; bitmap already written into buf[32:64]
	binary.LittleEndian.PutUint32(pg.buf[28:], nvsCRC32(pg.buf[4:28]))
}

// Partition accumulates entries and renders a fixed-size NVS image.
type Partition struct {
	size  int
	pages []*page
	nss   map[string]uint8
	nsN   uint8
}

// New creates an empty partition of the given byte size (multiple of 4096, min 0x3000).
func New(size int) (*Partition, error) {
	if size%pageSize != 0 || size < 3*pageSize {
		return nil, fmt.Errorf("nvs: size must be a multiple of 4096 and >= 0x3000, got %d", size)
	}
	return &Partition{size: size, nss: map[string]uint8{}, pages: []*page{newPage(0)}}, nil
}

func (p *Partition) cur() *page { return p.pages[len(p.pages)-1] }

// fit returns a page guaranteed to have span free entries, advancing if needed.
func (p *Partition) fit(span int) (*page, error) {
	if span > entryCount {
		return nil, fmt.Errorf("nvs: item needs %d entries, exceeds one page (%d)", span, entryCount)
	}
	pg := p.cur()
	if pg.free() < span {
		pg = newPage(pg.seq + 1)
		p.pages = append(p.pages, pg)
	}
	return pg, nil
}

func (p *Partition) namespace(name string) (uint8, error) {
	if idx, ok := p.nss[name]; ok {
		return idx, nil
	}
	p.nsN++
	idx := p.nsN
	if err := p.writePrimitive(nsTableIndex, name, typeU8, uint64(idx), 1); err != nil {
		return 0, err
	}
	p.nss[name] = idx
	return idx, nil
}

func putKey(e []byte, key string) error {
	if len(key) > 15 {
		return fmt.Errorf("nvs: key %q exceeds 15 chars", key)
	}
	var k [16]byte // zero-padded
	copy(k[:], key)
	copy(e[8:24], k[:])
	return nil
}

func setEntryCRC(e []byte) {
	in := make([]byte, 0, 28)
	in = append(in, e[0:4]...)
	in = append(in, e[8:32]...)
	binary.LittleEndian.PutUint32(e[4:8], nvsCRC32(in))
}

func (p *Partition) writePrimitive(nsIdx uint8, key string, typ byte, val uint64, width int) error {
	pg, err := p.fit(1)
	if err != nil {
		return err
	}
	e := make([]byte, entrySize)
	for i := range e {
		e[i] = 0xFF
	}
	e[0] = nsIdx
	e[1] = typ
	e[2] = 1 // span
	e[3] = chunkAny
	if err := putKey(e, key); err != nil {
		return err
	}
	var v [8]byte
	binary.LittleEndian.PutUint64(v[:], val)
	copy(e[24:24+width], v[:width]) // remaining data bytes stay 0xFF
	setEntryCRC(e)
	pg.writeEntry(e)
	return nil
}

func (p *Partition) writeVar(nsIdx uint8, key string, typ byte, data []byte) error {
	dataEntries := (len(data) + entrySize - 1) / entrySize
	span := 1 + dataEntries
	pg, err := p.fit(span)
	if err != nil {
		return err
	}
	e := make([]byte, entrySize)
	for i := range e {
		e[i] = 0xFF
	}
	e[0] = nsIdx
	e[1] = typ
	e[2] = byte(span)
	e[3] = chunkAny
	if err := putKey(e, key); err != nil {
		return err
	}
	binary.LittleEndian.PutUint16(e[24:], uint16(len(data))) // size
	// e[26:28] reserved (0xFF)
	binary.LittleEndian.PutUint32(e[28:], nvsCRC32(data)) // data CRC
	setEntryCRC(e)
	pg.writeEntry(e)

	for i := 0; i < dataEntries; i++ {
		de := make([]byte, entrySize)
		for j := range de {
			de[j] = 0xFF
		}
		end := min((i+1)*entrySize, len(data))
		copy(de, data[i*entrySize:end]) // 0xFF-padded
		pg.writeEntry(de)
	}
	return nil
}

func (p *Partition) setInt(ns, key string, typ byte, val uint64, width int) error {
	idx, err := p.namespace(ns)
	if err != nil {
		return err
	}
	return p.writePrimitive(idx, key, typ, val, width)
}

// Typed setters.
func (p *Partition) SetU8(ns, key string, v uint8) error   { return p.setInt(ns, key, typeU8, uint64(v), 1) }
func (p *Partition) SetU16(ns, key string, v uint16) error { return p.setInt(ns, key, typeU16, uint64(v), 2) }
func (p *Partition) SetU32(ns, key string, v uint32) error { return p.setInt(ns, key, typeU32, uint64(v), 4) }
func (p *Partition) SetU64(ns, key string, v uint64) error { return p.setInt(ns, key, typeU64, v, 8) }
func (p *Partition) SetI8(ns, key string, v int8) error    { return p.setInt(ns, key, typeI8, uint64(int64(v)), 1) }
func (p *Partition) SetI16(ns, key string, v int16) error  { return p.setInt(ns, key, typeI16, uint64(int64(v)), 2) }
func (p *Partition) SetI32(ns, key string, v int32) error  { return p.setInt(ns, key, typeI32, uint64(int64(v)), 4) }
func (p *Partition) SetI64(ns, key string, v int64) error  { return p.setInt(ns, key, typeI64, uint64(v), 8) }

// SetString stores a NUL-terminated string (NVS appends the NUL).
func (p *Partition) SetString(ns, key, val string) error {
	idx, err := p.namespace(ns)
	if err != nil {
		return err
	}
	return p.writeVar(idx, key, typeStr, append([]byte(val), 0))
}

// SetBlob stores a binary blob. The blob must fit within a single page (no
// cross-page chunking yet) — ample for keys/certs; large blobs return an error.
func (p *Partition) SetBlob(ns, key string, data []byte) error {
	idx, err := p.namespace(ns)
	if err != nil {
		return err
	}
	return p.writeVar(idx, key, typeBlob, data)
}

// Bytes renders the partition image: data pages followed by erased (0xFF) pages,
// with one trailing page reserved (as the generator does).
func (p *Partition) Bytes() ([]byte, error) {
	if usable := p.size/pageSize - 1; len(p.pages) > usable {
		return nil, fmt.Errorf("nvs: data needs %d pages, partition holds %d", len(p.pages), usable)
	}
	out := make([]byte, p.size)
	for i := range out {
		out[i] = 0xFF
	}
	for i, pg := range p.pages {
		state := uint32(stateActive)
		if i < len(p.pages)-1 {
			state = stateFull
		}
		pg.finalize(state)
		copy(out[i*pageSize:], pg.buf[:])
	}
	return out, nil
}
