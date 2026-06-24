package esp

import (
	"encoding/binary"
	"fmt"
	"math/bits"
)

// GET_SECURITY_INFO flag bits (esptool).
const (
	secFlagSecureBootEn     = 1 << 0
	secFlagSecureBootAggRev = 1 << 1
	secFlagSecureDownloadEn = 1 << 2
	secFlagKeyRevoke0       = 1 << 3
	secFlagKeyRevoke1       = 1 << 4
	secFlagKeyRevoke2       = 1 << 5
)

// keyPurposeNames maps eFuse key-block purpose ids to labels (ESP32 key purposes).
var keyPurposeNames = map[byte]string{
	0:  "USER/empty",
	1:  "RESERVED",
	2:  "XTS_AES_256_KEY_1",
	3:  "XTS_AES_256_KEY_2",
	4:  "XTS_AES_128_KEY",
	5:  "HMAC_DOWN_ALL",
	6:  "HMAC_DOWN_JTAG",
	7:  "HMAC_DOWN_DIGITAL_SIGNATURE",
	8:  "HMAC_UP",
	9:  "SECURE_BOOT_DIGEST0",
	10: "SECURE_BOOT_DIGEST1",
	11: "SECURE_BOOT_DIGEST2",
	12: "ECDSA_KEY",
}

// KeyPurposeName returns a human label for a key-block purpose id.
func KeyPurposeName(p byte) string {
	if n, ok := keyPurposeNames[p]; ok {
		return n
	}
	return fmt.Sprintf("unknown(%d)", p)
}

// SecurityInfo is the decoded GET_SECURITY_INFO response: the device's secure-boot
// / flash-encryption / download-protection state and eFuse key-block purposes.
type SecurityInfo struct {
	Flags         uint32
	FlashCryptCnt byte
	KeyPurposes   [7]byte
	ChipID        uint32
	APIVersion    uint32
	HasExtended   bool // chip id + api version present
}

// SecureBoot reports whether Secure Boot v2 is enabled.
func (s *SecurityInfo) SecureBoot() bool { return s.Flags&secFlagSecureBootEn != 0 }

// SecureDownload reports whether secure download mode is enabled.
func (s *SecurityInfo) SecureDownload() bool { return s.Flags&secFlagSecureDownloadEn != 0 }

// FlashEncryption reports whether flash encryption is enabled (odd crypt count).
func (s *SecurityInfo) FlashEncryption() bool { return bits.OnesCount8(s.FlashCryptCnt)%2 == 1 }

// KeyRevocations reports which Secure Boot key digests have been revoked.
func (s *SecurityInfo) KeyRevocations() (r0, r1, r2 bool) {
	return s.Flags&secFlagKeyRevoke0 != 0, s.Flags&secFlagKeyRevoke1 != 0, s.Flags&secFlagKeyRevoke2 != 0
}

// SecurityInfo reads and decodes GET_SECURITY_INFO from the chip.
func (l *Loader) SecurityInfo() (*SecurityInfo, error) {
	resp, err := l.command(cmdGetSecurityInfo, nil, 0, cmdTimeout)
	if err != nil {
		return nil, err
	}
	p := resp.payload(l.stub)
	if len(p) < 12 {
		return nil, fmt.Errorf("esp: security info too short (%d bytes)", len(p))
	}
	si := &SecurityInfo{
		Flags:         binary.LittleEndian.Uint32(p[0:4]),
		FlashCryptCnt: p[4],
	}
	copy(si.KeyPurposes[:], p[5:12])
	if len(p) >= 20 {
		si.ChipID = binary.LittleEndian.Uint32(p[12:16])
		si.APIVersion = binary.LittleEndian.Uint32(p[16:20])
		si.HasExtended = true
	}
	return si, nil
}
