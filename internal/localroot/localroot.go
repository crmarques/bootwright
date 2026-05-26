package localroot

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	InternalEnv   = "BOOTWRIGHT_INTERNAL_LOCAL_ROOT"
	CallerHomeEnv = "BOOTWRIGHT_INTERNAL_CALLER_HOME"
)

func IsInternalRootChild() bool {
	return strings.TrimSpace(os.Getenv(InternalEnv)) == "1" && os.Geteuid() == 0
}

func CallerHomeDir() (string, bool) {
	if strings.TrimSpace(os.Getenv(InternalEnv)) != "1" {
		return "", false
	}
	home := strings.TrimSpace(os.Getenv(CallerHomeEnv))
	if home == "" {
		return "", false
	}
	return filepath.Clean(home), true
}

func CallerUIDGID() (uint32, uint32, bool) {
	if !IsInternalRootChild() {
		return 0, 0, false
	}
	uidRaw := strings.TrimSpace(os.Getenv("SUDO_UID"))
	gidRaw := strings.TrimSpace(os.Getenv("SUDO_GID"))
	if uidRaw == "" || gidRaw == "" {
		return 0, 0, false
	}
	uid, uidErr := strconv.ParseUint(uidRaw, 10, 32)
	gid, gidErr := strconv.ParseUint(gidRaw, 10, 32)
	if uidErr != nil || gidErr != nil {
		return 0, 0, false
	}
	return uint32(uid), uint32(gid), true
}
