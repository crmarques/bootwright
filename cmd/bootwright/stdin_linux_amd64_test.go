//go:build linux && amd64

package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

func TestTerminalFileSelectsOnlyTerminalStreams(t *testing.T) {
	_, slave := openTestTerminal(t)
	if file, ok := terminalFile(slave); !ok || file != slave {
		t.Fatal("terminal stream not selected", ok, file)
	}
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer read.Close()
	defer write.Close()
	if _, ok := terminalFile(write); ok {
		t.Fatal("pipe selected as a terminal")
	}
	if _, ok := terminalFile(&bytes.Buffer{}); ok {
		t.Fatal("buffer selected as a terminal")
	}
}

// The progress row is redrawn within the width the terminal reports, so the
// reader follows a resize and a terminal that reports no size falls back to the
// classic width rather than to an unbounded row.
func TestTerminalColumnsFollowTheWindowSize(t *testing.T) {
	_, slave := openTestTerminal(t)
	width := terminalColumns(slave)
	if width == nil {
		t.Fatal("terminal reported no width reader")
	}
	if columns := width(); columns != defaultTerminalColumns {
		t.Fatalf("unsized terminal columns = %d, want %d", columns, defaultTerminalColumns)
	}
	window := struct{ Rows, Columns, XPixels, YPixels uint16 }{Rows: 24, Columns: 132}
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, slave.Fd(), syscall.TIOCSWINSZ, uintptr(unsafe.Pointer(&window))); errno != 0 {
		t.Fatalf("set window size: %v", errno)
	}
	if columns := width(); columns != int(window.Columns) {
		t.Fatalf("resized terminal columns = %d, want %d", columns, window.Columns)
	}
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer read.Close()
	defer write.Close()
	if terminalColumns(write) != nil {
		t.Fatal("pipe reported a terminal width")
	}
}

// The monitor helper plays sudo's monitor: session leader in the foreground of
// the pseudo-terminal, with the command helper parked in a background process
// group exactly as sudo starts a command whose output is not the user's tty.
func TestBackgroundConfirmationRequestsTerminalThroughJobControl(t *testing.T) {
	master, slave := openTestTerminal(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	monitor := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestTerminalMonitorHelper$")
	monitor.Env = append(os.Environ(), "BOOTWRIGHT_TERMINAL_MONITOR_HELPER=1")
	monitor.Stdin = slave
	var stderr bytes.Buffer
	monitor.Stderr = &stderr
	monitor.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	events, err := monitor.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := monitor.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if monitor.ProcessState == nil {
			cancel()
			_ = monitor.Wait()
		}
	})
	reader := bufio.NewReader(events)
	expect := func(want string) {
		t.Helper()
		line, err := reader.ReadString('\n')
		if err != nil || line != want+"\n" {
			cancel()
			_ = monitor.Wait()
			t.Fatalf("event %q want %q: %v stderr=%s", line, want, err, stderr.String())
		}
	}
	expect("stopped:SIGTTIN")
	if _, err := master.Write([]byte("y\n")); err != nil {
		t.Fatal(err)
	}
	expect("read:y")
	expect("exit:0")
	if err := monitor.Wait(); err != nil {
		t.Fatal(err, stderr.String())
	}
}

func TestTerminalMonitorHelper(t *testing.T) {
	if os.Getenv("BOOTWRIGHT_TERMINAL_MONITOR_HELPER") != "1" {
		return
	}
	command := exec.Command(os.Args[0], "-test.run=^TestTerminalCommandHelper$")
	command.Env = append(os.Environ(), "BOOTWRIGHT_TERMINAL_COMMAND_HELPER=1")
	command.Stdin, command.Stdout, command.Stderr = os.Stdin, os.Stdout, os.Stderr
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := command.Start(); err != nil {
		fmt.Println("start:", err)
		os.Exit(1)
	}
	pid := command.Process.Pid
	var status syscall.WaitStatus
	if _, err := syscall.Wait4(pid, &status, syscall.WUNTRACED, nil); err != nil || !status.Stopped() || status.StopSignal() != syscall.SIGTTIN {
		fmt.Printf("stopped:none %v %v\n", err, status)
		os.Exit(1)
	}
	fmt.Println("stopped:SIGTTIN")
	group := int32(pid)
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, os.Stdin.Fd(), syscall.TIOCSPGRP, uintptr(unsafe.Pointer(&group))); errno != 0 {
		fmt.Println("grant:", errno)
		os.Exit(1)
	}
	if err := syscall.Kill(pid, syscall.SIGCONT); err != nil {
		fmt.Println("continue:", err)
		os.Exit(1)
	}
	if _, err := syscall.Wait4(pid, &status, 0, nil); err != nil {
		fmt.Println("wait:", err)
		os.Exit(1)
	}
	fmt.Printf("exit:%d\n", status.ExitStatus())
	os.Exit(0)
}

func TestTerminalCommandHelper(t *testing.T) {
	if os.Getenv("BOOTWRIGHT_TERMINAL_COMMAND_HELPER") != "1" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var answer []byte
	for {
		var next [1]byte
		if _, err := readStdin(ctx, next[:]); err != nil {
			fmt.Println("error:", err)
			os.Exit(1)
		}
		if next[0] == '\n' {
			break
		}
		answer = append(answer, next[0])
	}
	fmt.Printf("read:%s\n", answer)
	os.Exit(0)
}

func TestConfirmationInputHonorsCancellationAndReadsNoAhead(t *testing.T) {
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer read.Close()
	defer write.Close()
	original := os.Stdin
	os.Stdin = read
	t.Cleanup(func() { os.Stdin = original })
	terminal, err := stdinTerminal()
	if err != nil || terminal {
		t.Fatal("pipe identified as interactive", terminal, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := readStdin(ctx, make([]byte, 1)); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("blocked read ignored cancellation", err)
	}
	if _, err := write.Write([]byte("yes\nnext\n")); err != nil {
		t.Fatal(err)
	}
	buffer := make([]byte, 1)
	if n, err := readStdin(context.Background(), buffer); err != nil || n != 1 || buffer[0] != 'y' {
		t.Fatal("read beyond requested boundary", n, string(buffer), err)
	}
	remaining := make([]byte, 8)
	if n, err := read.Read(remaining); err != nil || n != 8 || string(remaining) != "es\nnext\n" {
		t.Fatal(n, string(remaining), err)
	}
}

func TestReadyInputRestoresFlagsOnReadFailure(t *testing.T) {
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer read.Close()
	defer write.Close()
	descriptor := write.Fd()
	before, _, errno := syscall.Syscall(syscall.SYS_FCNTL, descriptor, syscall.F_GETFL, 0)
	if errno != 0 {
		t.Fatal(errno)
	}
	if _, err := readReadyInput(int(descriptor), make([]byte, 1)); err != syscall.EBADF {
		t.Fatal(err)
	}
	after, _, errno := syscall.Syscall(syscall.SYS_FCNTL, descriptor, syscall.F_GETFL, 0)
	if errno != 0 || after != before {
		t.Fatal("read failure changed descriptor flags", before, after, errno)
	}
}
