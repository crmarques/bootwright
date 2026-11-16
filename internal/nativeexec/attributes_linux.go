//go:build linux

package nativeexec

import "syscall"

func attributes(command Command) *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: !command.Foreground, Credential: command.Credential, Pdeathsig: command.ParentDeath}
}
