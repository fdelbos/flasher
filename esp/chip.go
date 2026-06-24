package esp

// ESP32-C6 register / eFuse layout (from esptool targets/esp32c6.py). The C6 does
// not use the legacy magic-register detection; identity comes from eFuse reads and
// GET_SECURITY_INFO.
const (
	c6EfuseBase   = 0x600B0800
	c6MacReg      = 0x600B0844 // EFUSE_BASE + 0x44 (BLOCK1): factory base MAC
	c6UartDateReg = 0x6000007C
)

// IMAGE_CHIP_ID values (esptool). Used to label a detected chip.
const (
	ChipIDESP32C6 = 13
)
