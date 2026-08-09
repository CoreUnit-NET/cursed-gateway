package cursor_api_sdk

import (
	"encoding/base64"
	"strings"
	"time"
)

// ChecksumHeader builds x-cursor-checksum for the given device ids.
func ChecksumHeader(ids DeviceIDs, now time.Time) string {
	n := now.UnixMilli() / 1_000_000
	ts := []byte{
		byte(n >> 40),
		byte(n >> 32),
		byte(n >> 24),
		byte(n >> 16),
		byte(n >> 8),
		byte(n),
	}
	prefix := strings.TrimRight(base64.StdEncoding.EncodeToString(obfuscate(ts)), "=")
	if ids.MacMachineID != "" {
		return prefix + ids.MachineID + "/" + ids.MacMachineID
	}
	return prefix + ids.MachineID
}

func obfuscate(in []byte) []byte {
	out := make([]byte, len(in))
	copy(out, in)
	a := byte(165)
	for i := range out {
		out[i] = ((out[i] ^ a) + byte(i)) & 0xff
		a = out[i]
	}
	return out
}
