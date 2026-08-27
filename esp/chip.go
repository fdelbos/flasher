package esp

// IMAGE_CHIP_ID values (esptool). Used to select the register layout + stub for
// a detected target.
const (
	ChipIDESP32S3 = 9
	ChipIDESP32C6 = 13
)

// Chip captures the only target-specific bits the loader needs: where the factory
// base MAC lives in eFuse, how ESP-IDF derives the per-interface MACs from it, and
// the flasher stub to upload. The rest of the ROM/serial protocol is chip-agnostic.
type Chip struct {
	ID            uint32
	Name          string
	MACEfuseReg   uint32 // register holding the low 32 bits of the factory base MAC
	UniversalMACs int    // 4 (STA/AP/BT/ETH universal) or 2 (STA/BT universal)
	stub          []byte // embedded esp-flasher-stub JSON for this target
}

// chips is the supported-target registry, keyed by IMAGE_CHIP_ID (from
// GET_SECURITY_INFO). eFuse MAC registers are EFUSE_BASE+0x44 per esptool's
// targets (C6 EFUSE_BASE 0x600B0800, S3 0x60007000).
var chips = map[uint32]*Chip{
	ChipIDESP32S3: {ID: ChipIDESP32S3, Name: "esp32s3", MACEfuseReg: 0x60007044, UniversalMACs: 2, stub: stubS3JSON},
	ChipIDESP32C6: {ID: ChipIDESP32C6, Name: "esp32c6", MACEfuseReg: 0x600B0844, UniversalMACs: 4, stub: stubC6JSON},
}

// ChipName maps an IMAGE_CHIP_ID to its target name, or "unknown".
func ChipName(id uint32) string {
	if c, ok := chips[id]; ok {
		return c.Name
	}
	return "unknown"
}
