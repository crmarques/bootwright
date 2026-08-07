package execution

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type User string

const (
	UserCaller           User = "caller"
	UserLocalRoot        User = "local-root"
	UserRemoteSSH        User = "remote-ssh"
	UserRemoteBecome     User = "remote-become"
	UserControllerBecome User = "controller-become"
)

type Policy struct {
	Name       string
	User       User
	NeedsRoot  bool
	UsesBecome bool
}

var (
	CallerRead       = Policy{Name: "caller-read", User: UserCaller}
	LocalRootState   = Policy{Name: "local-root-state", User: UserLocalRoot, NeedsRoot: true}
	RemoteSSH        = Policy{Name: "remote-ssh", User: UserRemoteSSH}
	RemoteBecome     = Policy{Name: "remote-become", User: UserRemoteBecome, UsesBecome: true}
	ControllerBecome = Policy{Name: "controller-become", User: UserControllerBecome, UsesBecome: true}
)

const (
	InternalEnv   = "BOOTWRIGHT_INTERNAL_LOCAL_ROOT"
	CallerHomeEnv = "BOOTWRIGHT_INTERNAL_CALLER_HOME"
	CallerPathEnv = "BOOTWRIGHT_INTERNAL_CALLER_PATH"
)

func LocalRootCommandArgs(registryEnv, registryPath, callerHome, callerPath, executable string, extraEnv []string, args []string) []string {
	out := []string{
		"env",
		registryEnv + "=" + registryPath,
		InternalEnv + "=1",
		CallerHomeEnv + "=" + callerHome,
		CallerPathEnv + "=" + callerPath,
	}
	out = append(out, extraEnv...)
	out = append(out, executable)
	return append(out, args...)
}

func IsInternalLocalRootChild() bool {
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

func CallerPath() (string, bool) {
	if strings.TrimSpace(os.Getenv(InternalEnv)) != "1" {
		return "", false
	}
	path := strings.TrimSpace(os.Getenv(CallerPathEnv))
	if path == "" {
		return "", false
	}
	return path, true
}

func CallerUserName() string {
	if !IsInternalLocalRootChild() {
		return ""
	}
	return strings.TrimSpace(os.Getenv("SUDO_USER"))
}

func CallerUIDGID() (uint32, uint32, bool) {
	if !IsInternalLocalRootChild() {
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
