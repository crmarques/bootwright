//go:build linux && amd64

package privilege

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/crmarques/bootwright/internal/nativeexec"
)

func (Resolver) Resolve(ctx context.Context) (Account, error) {
	if err := ctx.Err(); err != nil {
		return Account{}, err
	}
	request := accountRequest{
		uid: os.Getuid(), euid: os.Geteuid(), sudoUID: os.Getenv("SUDO_UID"), sudoGID: os.Getenv("SUDO_GID"), sudoUser: os.Getenv("SUDO_USER"),
		sudoCommand: os.Getenv("SUDO_COMMAND"),
	}
	sudoParentPID := 0
	if request.euid == 0 && (request.sudoUID != "" || request.sudoGID != "" || request.sudoUser != "") {
		sudoParentPID = os.Getppid()
		request.trustedParent, request.selfCommand = sudoParent(request.sudoCommand)
	}
	directory, err := accountDirectoryAt(systemGetent, qualifiedSystemExecutable, systemAccountFiles)
	if err != nil {
		return Account{}, err
	}
	if err := ctx.Err(); err != nil {
		return Account{}, err
	}
	account, err := accountFrom(ctx, directory, request)
	if err == nil && request.trustedParent {
		if os.Getppid() != sudoParentPID {
			return Account{}, accountRefusal{reexecutionOrphaned}
		}
		account.SudoParentPID = sudoParentPID
	}
	return account, err
}

func sudoParent(sudoCommand string) (trusted, self bool) {
	sudo, err := QualifiedSudo()
	parent, parentErr := os.Stat("/proc/" + strconv.Itoa(os.Getppid()) + "/exe")
	qualified, qualifiedErr := os.Stat(sudo)
	trusted = err == nil && parentErr == nil && qualifiedErr == nil && os.SameFile(parent, qualified)
	path, _, _ := strings.Cut(sudoCommand, " ")
	invoked, invokedErr := os.Stat(path)
	running, runningErr := os.Stat("/proc/self/exe")
	return trusted, filepath.IsAbs(path) && invokedErr == nil && runningErr == nil && os.SameFile(invoked, running)
}

func systemAccountFiles() (accountDirectory, error) {
	passwd, err := readAccountFile("/etc/passwd")
	if err != nil {
		return nil, err
	}
	groups, err := readAccountFile("/etc/group")
	if err != nil {
		return nil, err
	}
	return accountFiles{passwd: passwd, groups: groups}, nil
}

func readAccountFile(path string) ([]byte, error) {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, accountRefusal{unansweredDirectory}
	}
	file := os.NewFile(uintptr(fd), path)
	defer file.Close()
	var before, after syscall.Stat_t
	if syscall.Fstat(fd, &before) != nil || before.Mode&syscall.S_IFMT != syscall.S_IFREG || before.Uid != 0 || before.Mode&0022 != 0 || before.Size < 0 || before.Size > 1<<20 {
		return nil, accountRefusal{unansweredDirectory}
	}
	data, err := io.ReadAll(io.LimitReader(file, (1<<20)+1))
	if err != nil || len(data) > 1<<20 || syscall.Fstat(fd, &after) != nil || before.Size != after.Size || before.Mtim != after.Mtim || before.Ctim != after.Ctim {
		return nil, accountRefusal{unansweredDirectory}
	}
	return data, nil
}

// QualifiedSudo selects the system executable without consulting PATH.
func QualifiedSudo() (string, error) {
	return qualifiedSystemExecutable("/usr/bin/sudo")
}

func qualifiedSystemExecutable(path string) (string, error) {
	qualified, err := nativeexec.Qualify(path)
	if err != nil {
		return "", errAccount
	}
	return qualified, nil
}
