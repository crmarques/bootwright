//go:build !linux

package comparisonlocal

import "syscall"

func processAttributes(credential *syscall.Credential) *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Credential: credential}
}
