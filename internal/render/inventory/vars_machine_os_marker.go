package inventory

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
)

func machineOSInstallMarkerVars(osInstall map[string]any, clusterName, machineName, profileName string) map[string]any {
	data, _ := json.Marshal(stableMarkerInput(osInstall))
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

func stableMarkerInput(osInstall map[string]any) map[string]any {
	out := deepCopyMarkerValue(osInstall).(map[string]any)
	if ssh, ok := out["ssh"].(map[string]any); ok {
		markerBasename(ssh, "privateKeyPath")
		markerBasename(ssh, "knownHostsPath")
		markerBasename(ssh, "trustDir")
	}
	if kickstart, ok := out["kickstart"].(map[string]any); ok {
		markerBasename(kickstart, "sshPublicKeyPath")
	}
	if installer, ok := out["installer"].(map[string]any); ok {
		if rhsm, ok := installer["rhsm"].(map[string]any); ok {
			markerBasename(rhsm, "organizationPath")
			markerBasename(rhsm, "activationKeyPath")
			if satellite, ok := rhsm["satellite"].(map[string]any); ok {
				markerBasename(satellite, "caPath")
			}
		}
		if proxy, ok := installer["proxy"].(map[string]any); ok {
			markerBasename(proxy, "credentialsPath")
		}
	}
	return out
}

func markerBasename(m map[string]any, key string) {
	if v, ok := m[key].(string); ok && v != "" {
		m[key] = filepath.Base(v)
	}
}

func deepCopyMarkerValue(v any) any {
	switch t := v.(type) {
	case map[string]any:
		m := make(map[string]any, len(t))
		for k, val := range t {
			m[k] = deepCopyMarkerValue(val)
		}
		return m
	case []any:
		s := make([]any, len(t))
		for i, val := range t {
			s[i] = deepCopyMarkerValue(val)
		}
		return s
	default:
		return v
	}
}
