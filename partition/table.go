package partition

import (
	"bytes"
	"encoding/binary"
	"fmt"
)

// Partition-table.bin layout: a sequence of 32-byte records. Data records start
// with magic 0xAA50; an optional MD5 record starts with 0xEBEB; the table ends
// at the first all-0xFF record (erased flash) or after MaxTableSize bytes.
const (
	entrySize    = 32
	entryMagic   = 0x50AA // bytes 0xAA 0x50 read little-endian
	md5Magic     = 0xEBEB
	MaxTableSize = 0xC00

	// Entry.Type values.
	TypeApp  = 0x00
	TypeData = 0x01
)

// Entry is one partition in a partition-table.bin.
type Entry struct {
	Name    string
	Type    byte
	SubType byte
	Offset  uint32
	Size    uint32
	Flags   uint32
}

// ParseTable decodes a partition-table.bin image.
func ParseTable(data []byte) ([]Entry, error) {
	var out []Entry
	for off := 0; off+entrySize <= len(data) && off < MaxTableSize; off += entrySize {
		rec := data[off : off+entrySize]
		magic := binary.LittleEndian.Uint16(rec[0:2])
		switch magic {
		case entryMagic:
			name := rec[12:28]
			if i := bytes.IndexByte(name, 0); i >= 0 {
				name = name[:i]
			}
			out = append(out, Entry{
				Name:    string(name),
				Type:    rec[2],
				SubType: rec[3],
				Offset:  binary.LittleEndian.Uint32(rec[4:8]),
				Size:    binary.LittleEndian.Uint32(rec[8:12]),
				Flags:   binary.LittleEndian.Uint32(rec[28:32]),
			})
		case md5Magic:
			continue // checksum record
		case 0xFFFF:
			if bytes.Count(rec, []byte{0xFF}) != entrySize {
				return nil, fmt.Errorf("partition table: bad record at offset %#x", off)
			}
			off = MaxTableSize // erased record: end of table
		default:
			return nil, fmt.Errorf("partition table: bad magic %#04x at offset %#x", magic, off)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("partition table: no entries")
	}
	return out, nil
}

// FindEntry returns the named partition, or nil.
func FindEntry(entries []Entry, name string) *Entry {
	for i := range entries {
		if entries[i].Name == name {
			return &entries[i]
		}
	}
	return nil
}
