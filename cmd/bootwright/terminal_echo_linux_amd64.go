//go:build linux && amd64

package main

import (
	"io"
	"syscall"
	"unsafe"
)

const echoFlags = syscall.ECHO | syscall.ECHONL

func terminalEchoHold(writer io.Writer) func() func() {
	file, ok := terminalFile(writer)
	if !ok {
		return nil
	}
	fd := int(file.Fd())
	return func() func() {
		var saved syscall.Termios
		if !terminalForeground(fd) || termiosControl(fd, syscall.TCGETS, &saved) != nil || saved.Lflag&echoFlags == 0 {
			return func() {}
		}
		held := saved
		held.Lflag &^= echoFlags
		if termiosControl(fd, syscall.TCSETS, &held) != nil {
			return func() {}
		}
		return func() { restoreEchoFlags(fd, saved.Lflag&echoFlags) }
	}
}

func preserveTerminalEcho(writer io.Writer) func() {
	file, ok := terminalFile(writer)
	if !ok {
		return func() {}
	}
	fd := int(file.Fd())
	var saved syscall.Termios
	if termiosControl(fd, syscall.TCGETS, &saved) != nil {
		return func() {}
	}
	return func() { restoreEchoFlags(fd, saved.Lflag&echoFlags) }
}

func restoreEchoFlags(fd int, flags uint32) {
	var current syscall.Termios
	if !terminalForeground(fd) || termiosControl(fd, syscall.TCGETS, &current) != nil || current.Lflag&echoFlags == flags {
		return
	}
	current.Lflag = current.Lflag&^echoFlags | flags
	termiosControl(fd, syscall.TCSETS, &current)
}

func terminalForeground(fd int) bool {
	var foreground int32
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), syscall.TIOCGPGRP, uintptr(unsafe.Pointer(&foreground)))
	return errno != 0 || int(foreground) == syscall.Getpgrp()
}
