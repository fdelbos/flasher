package esp

// SLIP framing (RFC 1055) as used by the ESP serial bootloader: every packet is
// delimited by 0xC0; inside the payload 0xC0 -> 0xDB 0xDC and 0xDB -> 0xDB 0xDD.

const (
	slipEnd     = 0xC0
	slipEsc     = 0xDB
	slipEscEnd  = 0xDC
	slipEscEsc  = 0xDD
)

// slipEncode wraps payload in a single SLIP frame.
func slipEncode(payload []byte) []byte {
	out := make([]byte, 0, len(payload)+2)
	out = append(out, slipEnd)
	for _, b := range payload {
		switch b {
		case slipEnd:
			out = append(out, slipEsc, slipEscEnd)
		case slipEsc:
			out = append(out, slipEsc, slipEscEsc)
		default:
			out = append(out, b)
		}
	}
	return append(out, slipEnd)
}
