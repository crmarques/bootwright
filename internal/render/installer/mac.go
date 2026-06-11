package installer

import (
	"crypto/sha1"
	"fmt"
)

func libvirtMACAddress(clusterName, machineName, interfaceName string) string {
	sum := sha1.Sum([]byte(clusterName + "\x00" + machineName + "\x00" + interfaceName))
	return fmt.Sprintf("52:54:00:%02x:%02x:%02x", sum[0], sum[1], sum[2])
}

// vsphereMACAddress derives a deterministic manually-assigned VM NIC MAC.
// vCenter accepts manual MACs only inside 00:50:56:00:00:00 through
// 00:50:56:3f:ff:ff, so the first hashed byte is masked into that range.
func vsphereMACAddress(clusterName, machineName, interfaceName string) string {
	sum := sha1.Sum([]byte(clusterName + "\x00" + machineName + "\x00" + interfaceName))
	return fmt.Sprintf("00:50:56:%02x:%02x:%02x", sum[0]&0x3f, sum[1], sum[2])
}
