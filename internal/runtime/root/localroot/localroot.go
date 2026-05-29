package localroot

import (
	"github.com/crmarques/bootwright/internal/runtime/root/execution"
)

const (
	InternalEnv   = execution.InternalEnv
	CallerHomeEnv = execution.CallerHomeEnv
	CallerPathEnv = execution.CallerPathEnv
)

func IsInternalRootChild() bool {
	return execution.IsInternalLocalRootChild()
}

func CallerHomeDir() (string, bool) {
	return execution.CallerHomeDir()
}

func CallerPath() (string, bool) {
	return execution.CallerPath()
}

func CallerUIDGID() (uint32, uint32, bool) {
	return execution.CallerUIDGID()
}
