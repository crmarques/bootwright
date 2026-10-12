//go:build linux && amd64

package ansiblerunner

import (
	"maps"
	"os"
	"os/exec"
)

func Isolated(playbooks map[string]string, jobParent, scratchParent string, adapter func() *exec.Cmd) Runner {
	return Runner{
		jobParent: jobParent, scratchParent: scratchParent, owner: uint32(os.Geteuid()),
		command:   func(string, ...string) *exec.Cmd { return adapter() },
		playbooks: maps.Clone(playbooks),
	}
}
