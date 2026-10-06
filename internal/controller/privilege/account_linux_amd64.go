//go:build linux && amd64

package privilege

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
)

func (Resolver) Resolve(ctx context.Context) (Account, error) {
	if err := ctx.Err(); err != nil {
		return Account{}, err
	}
	request := accountRequest{
		uid: os.Getuid(), euid: os.Geteuid(), sudoUID: os.Getenv("SUDO_UID"), sudoGID: os.Getenv("SUDO_GID"), sudoUser: os.Getenv("SUDO_USER"),
		sudoCommand: os.Getenv("SUDO_COMMAND"),
	}
	sudoParent := 0
	if request.euid == 0 && (request.sudoUID != "" || request.sudoGID != "" || request.sudoUser != "") {
		sudo, err := QualifiedSudo()
		sudoParent = os.Getppid()
		parent, parentErr := os.Stat("/proc/" + strconv.Itoa(sudoParent) + "/exe")
		trusted, trustedErr := os.Stat(sudo)
		request.trustedParent = err == nil && parentErr == nil && trustedErr == nil && os.SameFile(parent, trusted)
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
		if os.Getppid() != sudoParent {
			return Account{}, accountRefusal{reexecutionOrphaned}
		}
		account.SudoParentPID = sudoParent
	}
	return account, err
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

// qualifiedSystemExecutable resolves path to an executable regular file root
// owns below directories root owns, none of them writable by a group or other
// account, so no account but root could have replaced what runs.
func qualifiedSystemExecutable(path string) (string, error) {
	path, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", errAccount
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0022 != 0 || info.Mode().Perm()&0111 == 0 {
		return "", errAccount
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != 0 {
		return "", errAccount
	}
	for parent := filepath.Dir(path); ; parent = filepath.Dir(parent) {
		info, err := os.Lstat(parent)
		if err != nil || !info.IsDir() || info.Mode().Perm()&0022 != 0 {
			return "", errAccount
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || stat.Uid != 0 {
			return "", errAccount
		}
		if parent == "/" {
			break
		}
	}
	return path, nil
}
