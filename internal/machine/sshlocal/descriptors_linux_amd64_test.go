//go:build linux && amd64

package sshlocal

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// This subprocess models OpenSSH closing inherited descriptors before it
// reads configuration, identities and pinned host keys. It never connects.
func TestSSHDescriptorClient(t *testing.T) {
	separator := slices.Index(os.Args, "--ssh-descriptor-client")
	if separator < 0 {
		return
	}
	args := os.Args[separator+1:]
	if err := unix.CloseRange(3, ^uint(0), 0); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(90)
	}
	var material []string
	var known string
	observe := false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-F", "-i":
			i++
			material = append(material, args[i])
		case "-o":
			i++
			if value, found := strings.CutPrefix(args[i], "UserKnownHostsFile="); found {
				known = value
			}
			observe = observe || args[i] == "StrictHostKeyChecking=accept-new"
		}
	}
	if !observe {
		material = append(material, known)
	}
	for _, path := range material {
		data, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(91)
		}
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != 0600 {
			os.Exit(92)
		}
		fmt.Printf("path=%s\n%s", path, data)
	}
	if observe {
		if err := os.WriteFile(known, []byte(hostKey().Line("198.51.100.11", 22)), 0600); err != nil {
			os.Exit(93)
		}
		os.Exit(255)
	}
	if slices.Contains(args, "wait") {
		fmt.Fprintln(os.Stdout, "ready")
		for {
			_ = unix.Pause()
		}
	}
	os.Exit(7)
}

func descriptorClient(t *testing.T) string {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return stubClient(t, "exec "+quoteWord(executable)+" -test.run=^TestSSHDescriptorClient$ -- --ssh-descriptor-client \"$@\"")
}

func TestSSHMaterialSurvivesClientDescriptorClosure(t *testing.T) {
	l := launcher(t, descriptorClient(t))
	l.PolicyPath = filepath.Join(t.TempDir(), "policy")
	const policy = "Ciphers aes256-gcm@openssh.com\n"
	if err := os.WriteFile(l.PolicyPath, []byte(policy), 0600); err != nil {
		t.Fatal(err)
	}
	s := keySession()
	s.IdentityFile = offeredKey(t, "OFFERED KEY\n")
	var out bytes.Buffer
	code, err := l.Run(context.Background(), s, nil, &out, &out)
	if err != nil || code != 7 {
		t.Fatalf("session exit = %d (%v): %s", code, err, out.String())
	}
	for _, want := range []string{policy, "OFFERED KEY\n", string(s.PrivateKey), s.HostKey.Line(s.Address, s.Port)} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("client did not read %q: %s", want, out.String())
		}
	}
	count := 0
	for _, line := range strings.Split(out.String(), "\n") {
		if path, found := strings.CutPrefix(line, "path="); found {
			count++
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatalf("session descriptor survived completion: %s (%v)", path, err)
			}
		}
	}
	if count != 4 {
		t.Fatalf("client read %d descriptors, want configuration, both keys and known hosts", count)
	}
	key, err := l.Observe(context.Background(), s.Address, s.Port)
	if err != nil || key != s.HostKey {
		t.Fatalf("observation = %+v (%v)", key, err)
	}
	if entries, err := os.ReadDir(l.Scratch); err != nil || len(entries) != 0 {
		t.Fatalf("session or observation material remained: %v (%v)", entries, err)
	}
}

// readyOutput signals only after the child has read all session material.
type readyOutput struct {
	buffer bytes.Buffer
	ready  chan struct{}
}

func (o *readyOutput) Write(data []byte) (int, error) {
	n, err := o.buffer.Write(data)
	if bytes.Contains(o.buffer.Bytes(), []byte("ready\n")) {
		select {
		case o.ready <- struct{}{}:
		default:
		}
	}
	return n, err
}

func (o *readyOutput) String() string { return o.buffer.String() }

func TestCancelledSSHSessionReleasesParentDescriptors(t *testing.T) {
	l := launcher(t, descriptorClient(t))
	s := keySession()
	s.Command = []string{"wait"}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out := &readyOutput{ready: make(chan struct{}, 1)}
	type outcome struct {
		code int
		err  error
	}
	done := make(chan outcome, 1)
	go func() {
		code, err := l.Run(ctx, s, nil, out, out)
		done <- outcome{code, err}
	}()
	select {
	case <-out.ready:
	case result := <-done:
		t.Fatalf("client exited before reading material: %+v: %s", result, out.String())
	case <-time.After(5 * time.Second):
		cancel()
		<-done
		t.Fatal("client did not read material")
	}
	cancel()
	result := <-done
	if result.err != nil || result.code != 143 {
		t.Fatalf("cancelled session = %+v: %s", result, out.String())
	}
	count := 0
	for _, line := range strings.Split(out.String(), "\n") {
		if path, found := strings.CutPrefix(line, "path="); found {
			count++
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatalf("cancelled session retained %s (%v)", path, err)
			}
		}
	}
	if count != 3 {
		t.Fatalf("client read %d descriptors, want configuration, key and known hosts", count)
	}
}
