package inventory

import (
	"crypto/sha1"
	"encoding/hex"
)

var ansibleUUIDNamespace = [16]byte{
	0x36, 0x1e, 0x6d, 0x51,
	0xfa, 0xec,
	0x44, 0x4a,
	0x90, 0x79,
	0x34, 0x13, 0x86, 0xda, 0x8e, 0x2e,
}

func ansibleUUIDv5(name string) string {
	h := sha1.New()
	h.Write(ansibleUUIDNamespace[:])
	h.Write([]byte(name))
	sum := h.Sum(nil)[:16]
	sum[6] = (sum[6] & 0x0f) | 0x50
	sum[8] = (sum[8] & 0x3f) | 0x80
	hexstr := hex.EncodeToString(sum)
	return hexstr[0:8] + "-" + hexstr[8:12] + "-" + hexstr[12:16] + "-" + hexstr[16:20] + "-" + hexstr[20:32]
}
