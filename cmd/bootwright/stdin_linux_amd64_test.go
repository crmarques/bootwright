//go:build linux && amd64

package main

import (
	"context"
	"errors"
	"os"
	"syscall"
	"testing"
	"time"
)

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
