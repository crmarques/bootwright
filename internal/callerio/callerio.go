package callerio

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/crmarques/bootwright/internal/localroot"
)

const (
	helperCommand = "__bootwright_internal_file"
	helperEnv     = "BOOTWRIGHT_INTERNAL_FILE_HELPER"
)

type statPayload struct {
	Name    string `json:"name"`
	Size    int64  `json:"size"`
	Mode    uint32 `json:"mode"`
	ModTime int64  `json:"modTime"`
	IsDir   bool   `json:"isDir"`
}

type FileInfo struct {
	payload statPayload
}

func (f FileInfo) Name() string       { return f.payload.Name }
func (f FileInfo) Size() int64        { return f.payload.Size }
func (f FileInfo) Mode() os.FileMode  { return os.FileMode(f.payload.Mode) }
func (f FileInfo) ModTime() time.Time { return time.Unix(0, f.payload.ModTime) }
func (f FileInfo) IsDir() bool        { return f.payload.IsDir }
func (f FileInfo) Sys() any           { return nil }

func RunHelper(args []string, stdout, stderr io.Writer) (int, bool) {
	if len(args) == 0 || args[0] != helperCommand {
		return 0, false
	}
	if os.Getenv(helperEnv) != "1" {
		fmt.Fprintln(stderr, "internal file helper is unavailable")
		return 2, true
	}
	if len(args) != 3 {
		fmt.Fprintln(stderr, "usage: internal file helper <read|stat> <path>")
		return 2, true
	}
	path := args[2]
	switch args[1] {
	case "read":
		data, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintf(stderr, "read %s: %v\n", path, err)
			return 1, true
		}
		if _, err := stdout.Write(data); err != nil {
			fmt.Fprintf(stderr, "write file helper output: %v\n", err)
			return 1, true
		}
		return 0, true
	case "stat":
		info, err := os.Stat(path)
		if err != nil {
			fmt.Fprintf(stderr, "stat %s: %v\n", path, err)
			return 1, true
		}
		payload := statPayload{
			Name:    info.Name(),
			Size:    info.Size(),
			Mode:    uint32(info.Mode()),
			ModTime: info.ModTime().UnixNano(),
			IsDir:   info.IsDir(),
		}
		if err := json.NewEncoder(stdout).Encode(payload); err != nil {
			fmt.Fprintf(stderr, "encode file helper stat: %v\n", err)
			return 1, true
		}
		return 0, true
	default:
		fmt.Fprintf(stderr, "unknown internal file helper action %q\n", args[1])
		return 2, true
	}
}

func ReadFile(path string) ([]byte, bool, error) {
	cmd, ok, err := bootwrightHelperCommand("read", path)
	if !ok || err != nil {
		return nil, ok, err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	data, err := cmd.Output()
	if err != nil {
		return nil, true, commandError("read", path, err, stderr.String())
	}
	return data, true, nil
}

func Stat(path string) (os.FileInfo, bool, error) {
	cmd, ok, err := bootwrightHelperCommand("stat", path)
	if !ok || err != nil {
		return nil, ok, err
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, true, commandError("stat", path, err, stderr.String())
	}
	var payload statPayload
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		return nil, true, fmt.Errorf("decode caller stat for %s: %w", path, err)
	}
	return FileInfo{payload: payload}, true, nil
}

func CommandOutput(name string, args ...string) ([]byte, bool, error) {
	cmd, ok, err := Command(name, args...)
	if !ok || err != nil {
		return nil, ok, err
	}
	out, err := cmd.CombinedOutput()
	return out, true, err
}

func Command(name string, args ...string) (*exec.Cmd, bool, error) {
	credential, ok, err := callerCredential()
	if !ok || err != nil {
		return nil, ok, err
	}
	resolved := name
	if !strings.ContainsRune(name, os.PathSeparator) {
		path, found, err := LookPath(name)
		if err != nil {
			return nil, true, err
		}
		if found {
			resolved = path
		}
	}
	cmd := exec.Command(resolved, args...)
	cmd.Env = callerEnv()
	cmd.SysProcAttr = &syscall.SysProcAttr{Credential: credential}
	return cmd, true, nil
}

func LookPath(name string) (string, bool, error) {
	if _, _, ok := localroot.CallerUIDGID(); !ok {
		return "", false, nil
	}
	if strings.ContainsRune(name, os.PathSeparator) {
		info, err := os.Stat(name)
		if err != nil {
			return "", true, err
		}
		if info.IsDir() || info.Mode()&0o111 == 0 {
			return "", true, exec.ErrNotFound
		}
		return filepath.Clean(name), true, nil
	}
	pathEnv, ok := localroot.CallerPath()
	if !ok {
		return "", true, exec.ErrNotFound
	}
	for _, dir := range filepath.SplitList(pathEnv) {
		if dir == "" {
			dir = "."
		}
		candidate := filepath.Join(dir, name)
		info, err := os.Stat(candidate)
		if err != nil || info.IsDir() || info.Mode()&0o111 == 0 {
			continue
		}
		return candidate, true, nil
	}
	return "", true, exec.ErrNotFound
}

func bootwrightHelperCommand(action, path string) (*exec.Cmd, bool, error) {
	credential, ok, err := callerCredential()
	if !ok || err != nil {
		return nil, ok, err
	}
	exe, err := os.Executable()
	if err != nil {
		return nil, true, fmt.Errorf("resolve bootwright executable for caller file %s: %w", action, err)
	}
	cmd := exec.Command(exe, helperCommand, action, path)
	cmd.Env = withEnv(callerEnv(), helperEnv, "1")
	cmd.SysProcAttr = &syscall.SysProcAttr{Credential: credential}
	return cmd, true, nil
}

func callerCredential() (*syscall.Credential, bool, error) {
	uid, gid, ok := localroot.CallerUIDGID()
	if !ok {
		return nil, false, nil
	}
	return &syscall.Credential{Uid: uid, Gid: gid, Groups: callerGroups(uid, gid)}, true, nil
}

func callerGroups(uid, gid uint32) []uint32 {
	groups := []uint32{gid}
	u, err := user.LookupId(strconv.FormatUint(uint64(uid), 10))
	if err != nil {
		return groups
	}
	ids, err := u.GroupIds()
	if err != nil {
		return groups
	}
	seen := map[uint32]bool{gid: true}
	for _, raw := range ids {
		id, err := strconv.ParseUint(raw, 10, 32)
		if err != nil {
			continue
		}
		group := uint32(id)
		if !seen[group] {
			groups = append(groups, group)
			seen[group] = true
		}
	}
	return groups
}

func callerEnv() []string {
	env := os.Environ()
	if home, ok := localroot.CallerHomeDir(); ok {
		env = withEnv(env, "HOME", home)
	}
	if path, ok := localroot.CallerPath(); ok {
		env = withEnv(env, "PATH", path)
	}
	return env
}

func withEnv(env []string, key, value string) []string {
	prefix := key + "="
	out := make([]string, 0, len(env)+1)
	for _, entry := range env {
		if !strings.HasPrefix(entry, prefix) {
			out = append(out, entry)
		}
	}
	return append(out, prefix+value)
}

func commandError(action, path string, err error, stderr string) error {
	stderr = string(bytes.TrimSpace([]byte(stderr)))
	if stderr == "" {
		return fmt.Errorf("%s %s as caller: %w", action, path, err)
	}
	return fmt.Errorf("%s %s as caller: %s: %w", action, path, stderr, err)
}
