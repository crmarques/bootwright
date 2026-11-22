package sshlocal

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"syscall"
)

const passwordDescriptor = "BOOTWRIGHT_SSH_PASSWORD_FD"

var passwordPath = regexp.MustCompile(`^/proc/[1-9][0-9]*/fd/[1-9][0-9]*$`)

func passwordEnvironment(password *os.File) []string {
	return []string{
		fmt.Sprintf("SSH_ASKPASS=/proc/%d/exe", os.Getpid()),
		"SSH_ASKPASS_REQUIRE=force",
		passwordDescriptor + "=" + descriptorPath(int(password.Fd())),
	}
}

// ServePassword is the askpass child of the isolated client. Its environment
// contains only an anonymous read handle. Material goes to the client's pipe.
func ServePassword(args []string) (bool, int) {
	path := os.Getenv(passwordDescriptor)
	if path == "" {
		return false, 0
	}
	if len(args) != 1 || os.Getuid() != os.Geteuid() || !passwordPath.MatchString(path) || !startedByClient(path, "/proc", os.Getppid(), Client) {
		return true, 1
	}
	if err := sendPassword(path, os.Stdout); err != nil {
		return true, 1
	}
	return true, 0
}

func startedByClient(path, proc string, ppid int, ssh string) bool {
	parent := fmt.Sprintf("%s/%d", proc, ppid)
	executable, err := os.Stat(parent + "/exe")
	client, clientErr := os.Stat(ssh)
	if err != nil || clientErr != nil || !os.SameFile(executable, client) {
		return false
	}
	status, err := os.ReadFile(parent + "/status")
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(status), "\n") {
		if pid, ok := strings.CutPrefix(line, "PPid:"); ok {
			return strings.HasPrefix(path, "/proc/"+strings.TrimSpace(pid)+"/fd/")
		}
	}
	return false
}

func sendPassword(path string, out io.Writer) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Size() < 1 || info.Size() > 4096 {
		return os.ErrInvalid
	}
	attributes, ok := info.Sys().(*syscall.Stat_t)
	if !ok || attributes.Nlink != 0 || attributes.Uid != uint32(os.Geteuid()) {
		return os.ErrInvalid
	}
	password, err := io.ReadAll(io.LimitReader(file, 4097))
	defer clear(password)
	if err != nil || int64(len(password)) != info.Size() || bytes.ContainsAny(password, "\x00\r\n") {
		return os.ErrInvalid
	}
	_, err = io.Copy(out, io.MultiReader(bytes.NewReader(password), strings.NewReader("\n")))
	return err
}
