package render

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

func machineOSInstallMarkerVars(osInstall map[string]any, clusterName, machineName, profileName string) map[string]any {
	data, _ := json.Marshal(osInstall)
	sum := sha256.Sum256(data)
	return map[string]any{
		"owner":       "bootwright",
		"cluster":     clusterName,
		"machine":     machineName,
		"profile":     profileName,
		"path":        "/etc/bootwright/install-marker.json",
		"desiredHash": "sha256:" + hex.EncodeToString(sum[:]),
	}
}
