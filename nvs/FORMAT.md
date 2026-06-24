# ESP-IDF NVS Partition Format — Implementation Reference

Byte-exact reference for generating an ESP-IDF NVS (Non-Volatile Storage)
partition image in pure Go (what the `nvs` package implements). Verified against
esp-idf 5.5 source **and** against real partitions produced by the official
`nvs_partition_gen.py`.

## Source files (esp-idf 5.5)

- Generator: `esp_idf_nvs_partition_gen/nvs_partition_gen.py` (the copy under
  `components/nvs_flash/nvs_partition_generator/` is a 3-line shim).
- `components/nvs_flash/src/nvs_types.hpp` — `Item` struct (entry layout).
- `components/nvs_flash/src/nvs_types.cpp` — entry CRC32.
- `components/nvs_flash/src/nvs_page.hpp` — `Header` struct, `PageState`/`EntryState`.
- `components/nvs_flash/src/nvs_page.cpp` — page-header CRC32.
- `components/nvs_flash/private_include/nvs_constants.h` — magic constants.
- `components/nvs_flash/include/nvs.h`, `include/nvs_handle.hpp` — `ItemType` bytes.

---

## 0. The two CRC32 computations (most critical)

Both CRCs are **CRC-32/ISO-HDLC (IEEE, reflected, poly 0xEDB88320)** with **init
0xFFFFFFFF** — `esp_rom_crc32_le(0xffffffff, ptr, len)` on-device,
`zlib.crc32(data, 0xFFFFFFFF) & 0xFFFFFFFF` in the generator.

```go
import "hash/crc32"
func nvsCRC32(data []byte) uint32 {
    return crc32.Update(0xffffffff, crc32.IEEETable, data)
}
```

Verified: `nvsCRC32([]byte("hi\x00")) == 0x0d04b589`.

WARNING: this is NOT `crc32.ChecksumIEEE(data)` (seed 0 →
`0x0dba9364`, wrong). Seed with `0xFFFFFFFF` via `crc32.Update`; the stored value
is the raw `Update` result (no extra final XOR).

---

## 1. Page layout (4096 bytes)

| Region | Offset | Size |
|---|---|---|
| Page header | 0 | 32 |
| Entry-state bitmap | 32 | 32 |
| 126 entries × 32 B | 64 | 4032 |

`ENTRY_SIZE=32`, `ENTRY_COUNT=126`, page = `SPI_FLASH_SEC_SIZE` = 4096.

---

## 2. Page header (32 bytes)

Order: `mState(4)`, `mSeqNumber(4)`, `mVersion(1)`, `mReserved[19]` (0xFF), `mCrc32(4)`.

| Offset | Size | Field | Value |
|---|---|---|---|
| 0 | 4 | page state | LE uint32 magic (below) |
| 4 | 4 | sequence number | **LE uint32**, starts 0, +1 per page |
| 8 | 1 | version | `0xFE` for V2 (default), `0xFF` for V1 |
| 9 | 19 | reserved | all **0xFF** |
| 28 | 4 | header CRC32 | LE uint32 |

**Page state magics** (LE):
- UNINITIALIZED = `0xFFFFFFFF`
- ACTIVE = `0xFFFFFFFE`
- FULL = `0xFFFFFFFC`
- FREEING = `0xFFFFFFF8`
- CORRUPT = `0xFFFFFFF0`

**Generator behavior:** each data page is written **ACTIVE**; when a data page
fills and a new one starts, the previous is rewritten **FULL**. So the last data
page is ACTIVE, earlier full ones are FULL. Trailing empty pages and the final
reserved page are left **fully erased (0xFF, no header written)**.

**Header CRC32:** covers bytes **[4:28]** (seqnum + version + reserved); excludes
state [0:4] and crc [28:32].
```
headerCRC = nvsCRC32(header[4:28])   // store LE at [28:32]
```

---

## 3. Entry-state bitmap (32 bytes, offset 32)

2 bits per entry × 126 = 252 bits → 32 bytes. Entry N → `bitnum = N*2`,
`byte = bitnum/8`, `bitoff = bitnum & 7` (lowest entry index = lowest bit).

**2-bit states:** EMPTY=`0b11`, WRITTEN=`0b10`, ERASED=`0b00`. The generator
clears one bit per written entry (`11`→`10`). So a generated page has WRITTEN
(`10`) for used entries, EMPTY (`11`) for the rest (3 used → byte0 = `0xAA`).

---

## 4. Entry (32 bytes)

| Offset | Size | Field |
|---|---|---|
| 0 | 1 | nsIndex (namespace index) |
| 1 | 1 | datatype (type byte) |
| 2 | 1 | span (number of 32-B entries incl. header) |
| 3 | 1 | chunkIndex (`0xFF` = CHUNK_ANY) |
| 4 | 4 | entry CRC32 (LE) |
| 8 | 16 | key (NUL-padded to 16; ≤15 chars + NUL) |
| 24 | 8 | data / metadata (§5, §6) |

**Type bytes:** U8=0x01, I8=0x11, U16=0x02, I16=0x12, U32=0x04, I32=0x14,
U64=0x08, I64=0x18, STR/SZ=0x21, BLOB(legacy)=0x41, BLOB_DATA=0x42,
BLOB_IDX=0x48, ANY=0xFF.

**Entry CRC32:** covers `entry[0:4] ++ entry[8:32]` (everything except the crc
field [4:8]).
```
entryCRC = nvsCRC32(entry[0:4] ++ entry[8:32])   // 28 bytes; store LE at [4:8]
```

---

## 5. Primitive values (data field at offset 24)

Integer packed **little-endian at offset 24** at natural width; remaining high
bytes keep their **0xFF** fill (NOT zero-padded). span=1, chunkIndex=0xFF.
Example: u8 42 → `2a ff ff ff ff ff ff ff`.

---

## 6. Variable-length items (string, blob)

**Head-entry data field (offset 24-31)** for single-page STR/BLOB:
- 24: `dataSize` uint16 LE
- 26: `reserved` uint16 (0xFFFF)
- 28: `dataCrc32` uint32 LE = `nvsCRC32(dataBytes)`

**String:** the generator appends a `\0`, so `datalen = len(utf8)+1` and the data
CRC covers the NUL. Type = SZ (0x21).

**span = 1 + ceil(datalen/32)** (header + data entries). Data bytes follow in the
next entries, final data entry **0xFF-padded** to 32 B.

**Blob chunking (V2, binary/hex2bin/base64):** data split into page-fitting
chunks. Each chunk → **BLOB_DATA (0x42)** entry: span = `1 + ceil(chunkSize/32)`,
chunkIndex = `chunkStart + chunkCount`, offset24 = chunkSize uint16 LE, offset28 =
chunk CRC32. After all chunks, one **BLOB_IDX (0x48)** entry: span=1,
chunkIndex=0xFF, offset24 = total dataSize uint32 LE, offset28 = chunkCount
(uint8), offset29 = chunkStart (uint8), 30-31 reserved. Strings are NOT chunked.

---

## 7. Namespaces

Stored as a **U8 entry under namespace index 0**:
- nsIndex=0, type=U8(0x01), key = namespace name, data byte24 = assigned index.

Indices start at **1**, increment per distinct namespace; duplicates reuse. Every
value entry for that namespace carries `nsIndex` = its assigned index.

---

## 8. CSV input (`key,type,encoding,value`)

- **type**: `namespace` (starts a namespace; key = name), `data` (literal value),
  `file` (value = path, contents read as bytes).
- **encoding**: `u8…i64` (int), `string` (NUL-appended, SZ), `hex2bin` (hex →
  bytes, BLOB), `base64`, `binary`.
- `#` lines skipped. Keys ≤15 chars. `--version` default 2. Size multiple of 4096;
  one trailing page reserved; min partition 0x3000.

---

## 9. Worked example (unit-test fixture)

CSV → `generate out.bin 0x3000 --version 2` (12288 bytes; page 0 data, pages 1-2 0xFF):
```
key,type,encoding,value
storage,namespace,,
x,data,u8,42
name,data,string,hi
```

**Page 0 header [0:32]:**
```
fe ff ff ff  00 00 00 00  fe ff ff ff  ff ff ff ff
ff ff ff ff  ff ff ff ff  ff ff ff ff  84 2d ba b9
```
state=ACTIVE, seq=0, version=FE, CRC `84 2d ba b9` = nvsCRC32(header[4:28]).

**Bitmap [32:64]:** `aa ff ff ff …` (entries 0,1,2 WRITTEN).

**Entry 0 — ns "storage" [64:96]:**
```
00 01 01 ff  09 a9 50 07  73 74 6f 72 61 67 65 00
00 00 00 00  00 00 00 00  01 ff ff ff  ff ff ff ff
```
nsIndex=0, U8, span=1, data24=`01` (ns index 1).

**Entry 1 — x=u8 42 [96:128]:**
```
01 01 01 ff  92 2c 49 14  78 00 00 00 00 00 00 00
00 00 00 00  00 00 00 00  2a ff ff ff  ff ff ff ff
```

**Entry 2 — name="hi" head [128:160]:**
```
01 21 02 ff  44 a5 41 b4  6e 61 6d 65 00 00 00 00
00 00 00 00  00 00 00 00  03 00 ff ff  89 b5 04 0d
```
SZ, span=2, dataSize=3, dataCrc `89 b5 04 0d` = nvsCRC32("hi\x00").

**Entry 3 — "hi\0" data [160:192]:** `68 69 00` then 0xFF to 32 bytes.
