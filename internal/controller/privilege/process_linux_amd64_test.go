//go:build linux && amd64

package privilege

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

func TestMain(m *testing.M) {
	if len(os.Args) == 2 {
		switch os.Args[1] {
		case "__bootwright_account_root", "__bootwright_account_user":
			uid, gid, home := 0, 0, "/root"
			if os.Args[1] == "__bootwright_account_user" {
				uid, gid, home = 60001, 60002, "/home/operator"
			}
			account, err := (Resolver{}).Resolve(context.Background())
			if err != nil || account.UID != uid || account.GID != gid || account.Home != home || account.SudoParentPID != 0 {
				os.Exit(2)
			}
			fmt.Println("verified")
			os.Exit(0)
		case "__bootwright_account_parent":
			child := exec.Command("/usr/bin/sudo", "__bootwright_account_child")
			child.Env = []string{"SUDO_UID=60001", "SUDO_GID=60002", "SUDO_USER=operator", "HOME=/incorrect"}
			child.Stdin = os.Stdin
			child.Stdout, child.Stderr = os.Stdout, os.Stderr
			if child.Run() != nil {
				os.Exit(2)
			}
			os.Exit(0)
		case "__bootwright_account_child":
			account, err := (Resolver{}).Resolve(context.Background())
			if err != nil || account.UID != 60001 || account.GID != 60002 || account.Home != "/home/operator" || account.Name != "operator" || account.SudoParentPID != os.Getppid() {
				os.Exit(2)
			}
			fmt.Println("verified")
			os.Exit(0)
		case "__bootwright_guard_child":
			release, err := GuardParent(os.Getppid())
			if err != nil {
				os.Exit(2)
			}
			defer release()
			thread := syscall.Gettid()
			for range 20 {
				runtime.GC()
				runtime.Gosched()
				time.Sleep(time.Millisecond)
				if syscall.Gettid() != thread {
					os.Exit(2)
				}
			}
			fmt.Println("ready")
			time.Sleep(time.Hour)
			os.Exit(3)
		case "__bootwright_guard_parent":
			self, err := os.Executable()
			if err != nil {
				os.Exit(2)
			}
			child := exec.Command(self, "__bootwright_guard_child")
			child.Stdout, child.Stderr = os.Stdout, os.Stderr
			if child.Start() != nil {
				os.Exit(2)
			}
			fmt.Printf("pid %d\n", child.Process.Pid)
			var release [1]byte
			if _, err := io.ReadFull(os.Stdin, release[:]); err != nil {
				os.Exit(2)
			}
			os.Exit(0)
		}
	}
	os.Exit(m.Run())
}

func TestGuardedCommandDiesAndIsReapedAfterParentExit(t *testing.T) {
	const getSubreaper, setSubreaper = 37, 36
	var previous int32
	_, _, errno := syscall.Syscall6(syscall.SYS_PRCTL, getSubreaper, uintptr(unsafe.Pointer(&previous)), 0, 0, 0, 0)
	if errno != 0 {
		t.Fatal(errno)
	}
	_, _, errno = syscall.Syscall6(syscall.SYS_PRCTL, setSubreaper, 1, 0, 0, 0, 0)
	if errno != 0 {
		t.Fatal(errno)
	}
	defer syscall.Syscall6(syscall.SYS_PRCTL, setSubreaper, uintptr(previous), 0, 0, 0, 0)
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	parent := exec.CommandContext(ctx, self, "__bootwright_guard_parent")
	parent.WaitDelay = time.Second
	input, err := parent.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	output, err := parent.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := parent.Start(); err != nil {
		t.Fatal(err)
	}
	childPID := 0
	defer func() {
		if parent.ProcessState == nil {
			parent.Process.Kill()
			parent.Wait()
		}
		if childPID != 0 {
			syscall.Kill(childPID, syscall.SIGKILL)
			var status syscall.WaitStatus
			syscall.Wait4(childPID, &status, 0, nil)
		}
	}()
	scanner := bufio.NewScanner(output)
	ready := false
	for scanner.Scan() {
		line := scanner.Text()
		if line == "ready" {
			ready = true
		}
		if strings.HasPrefix(line, "pid ") {
			childPID, _ = strconv.Atoi(strings.TrimPrefix(line, "pid "))
		}
		if ready && childPID > 0 {
			break
		}
	}
	if !ready || childPID <= 0 {
		t.Fatal("guard fixture did not start")
	}
	if _, err := input.Write([]byte{1}); err != nil {
		t.Fatal(err)
	}
	input.Close()
	if err := parent.Wait(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		var status syscall.WaitStatus
		pid, err := syscall.Wait4(childPID, &status, syscall.WNOHANG, nil)
		if err != nil {
			t.Fatal(err)
		}
		if pid == childPID {
			childPID = 0
			if !status.Signaled() || status.Signal() != syscall.SIGKILL {
				t.Fatalf("child status %v", status)
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("guarded child survived its parent")
}

func TestGuardRejectsChangedParent(t *testing.T) {
	if release, err := GuardParent(os.Getppid() + 1); err == nil {
		release()
		t.Fatal("incorrect parent accepted")
	}
}

func TestReexecutionPathPinsRunningExecutable(t *testing.T) {
	path, err := ReexecutionPath()
	if err != nil {
		t.Fatal(err)
	}
	if path != "/proc/"+strconv.Itoa(os.Getpid())+"/exe" {
		t.Fatal("reexecution is not bound to the supervisor")
	}
	selected, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	self, err := os.Stat("/proc/self/exe")
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(selected, self) {
		t.Fatal("reexecution selected another executable")
	}
}

// Run with a statically linked test binary in a private user/mount namespace
// and BOOTWRIGHT_PRIVILEGED_ACCOUNT_FIXTURE=1. Ordinary root test runs do not
// imply the mount capabilities or isolation this fixture requires.
// The fixture has its own account database and verified sudo-parent executable;
// no real account, home, sudo policy, credential cache or state store is changed.
func TestRootManualSudoAccountProvenanceFixture(t *testing.T) {
	if os.Geteuid() != 0 || os.Getenv("BOOTWRIGHT_PRIVILEGED_ACCOUNT_FIXTURE") != "1" {
		t.Skip("requires explicit privileged account fixture opt-in and an isolated root user/mount namespace")
	}
	root := t.TempDir()
	if err := os.Chmod(root, 0755); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"etc", "usr/bin", "proc"} {
		if err := os.MkdirAll(filepath.Join(root, path), 0755); err != nil {
			t.Fatal(err)
		}
	}
	for name, data := range map[string]string{
		"passwd": "root:x:0:0::/root:/bin/sh\noperator:x:60001:60002::/home/operator:/bin/sh\n",
		"group":  "root:x:0:\noperator:x:60002:\n",
	} {
		if err := os.WriteFile(filepath.Join(root, "etc", name), []byte(data), 0644); err != nil {
			t.Fatal(err)
		}
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"sudo", "launcher"} {
		if err := os.WriteFile(filepath.Join(root, "usr/bin", name), data, 0755); err != nil {
			t.Fatal(err)
		}
	}
	proc := filepath.Join(root, "proc")
	if err := syscall.Mount("/proc", proc, "", syscall.MS_BIND|syscall.MS_REC, ""); err != nil {
		t.Fatal(err)
	}
	defer syscall.Unmount(proc, syscall.MNT_DETACH)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, test := range []struct {
		name, executable, mode string
		user                   bool
		deny                   bool
	}{
		{"manual sudo", "sudo", "__bootwright_account_parent", false, false},
		{"untrusted parent", "launcher", "__bootwright_account_parent", false, true},
		{"direct root", "sudo", "__bootwright_account_root", false, false},
		{"nonroot spoofed metadata", "sudo", "__bootwright_account_user", true, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			command := exec.CommandContext(ctx, "/usr/bin/"+test.executable, test.mode)
			command.SysProcAttr = &syscall.SysProcAttr{Chroot: root}
			command.Env = []string{"LANG=C", "HOME=/incorrect"}
			if test.user {
				command.SysProcAttr.Credential = &syscall.Credential{Uid: 60001, Gid: 60002, Groups: []uint32{60002}}
				command.Env = append(command.Env, "SUDO_UID=0", "SUDO_GID=0", "SUDO_USER=root")
			}
			output, err := command.CombinedOutput()
			if test.deny {
				if err == nil {
					t.Fatal("untrusted sudo metadata accepted")
				}
				return
			}
			if err != nil || string(output) != "verified\n" {
				t.Fatalf("account provenance fixture: %v %q", err, output)
			}
		})
	}
}
