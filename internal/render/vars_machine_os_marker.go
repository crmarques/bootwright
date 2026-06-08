package render

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

// stableMarkerInput returns a deep copy of the rendered managed-OS install vars
// with the controller-side secret PATHS reduced to their stable basenames.
//
// privateKeyPath, sshPublicKeyPath, knownHostsPath, trustDir, and the RHSM
// organization/activation-key paths all resolve into the per-run runtime secrets
// directory (runs/history/<runID>/.../secrets). Hashing them verbatim made the
// on-host install marker change on every apply, so a re-apply of an already
// installed machine failed the role's "refuse to reinstall without a matching
// marker" guard, and --override then wiped and reinstalled with unchanged desired
// state. The basename is the stable secret material name, so the marker still
// changes when the referenced material changes but is identical across runs.
//
// The copy is essential: the original map still feeds vars.json, which must carry
// the real per-run paths for Ansible.
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
