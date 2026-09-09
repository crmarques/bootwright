//go:build linux && amd64

package selectionfs

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

const HelperMode = "__bootwright_selection"

type helperRequest struct {
	Account   Options            `json:"account"`
	Action    string             `json:"action"`
	Selection contexts.Selection `json:"selection"`
}

type helperResponse struct {
	Selection contexts.Selection `json:"selection"`
	Failed    bool               `json:"failed"`
}

func (s *Store) perform(ctx context.Context, action string, selection contexts.Selection) (contexts.Selection, error) {
	if err := ctx.Err(); err != nil {
		return contexts.Selection{}, err
	}
	if os.Geteuid() == s.options.UID {
		return s.local(ctx, action, selection)
	}
	if os.Geteuid() != 0 || s.options.UID <= 0 || s.options.GID < 0 {
		return contexts.Selection{}, state("selection account identity is unavailable")
	}
	executable := s.options.Executable
	if !filepath.IsAbs(executable) || filepath.Clean(executable) != executable {
		return contexts.Selection{}, state("selection helper executable is unavailable")
	}
	// This is the already-running program, never an executable supplied by a
	// selection record. Keeping the same file identity also rejects replacement.
	self, err := os.Stat("/proc/self/exe")
	info, infoErr := os.Lstat(executable)
	if err != nil || infoErr != nil || !info.Mode().IsRegular() || !os.SameFile(self, info) {
		return contexts.Selection{}, state("selection helper executable changed")
	}
	options := s.options
	options.Executable = ""
	data, err := json.Marshal(helperRequest{Account: options, Action: action, Selection: selection})
	if err != nil || len(data) > maximumRecord {
		return contexts.Selection{}, state("selection helper request exceeds its limit")
	}
	operation, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	command := exec.CommandContext(operation, executable, HelperMode)
	command.Env = []string{"LANG=C", "LC_ALL=C"}
	command.Dir = "/"
	command.Stdin = bytes.NewReader(data)
	command.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: uint32(options.UID), Gid: uint32(options.GID), Groups: append([]uint32(nil), options.Groups...)}, Pdeathsig: syscall.SIGKILL}
	var output boundedBuffer
	command.Stdout = &output
	command.Stderr = io.Discard
	command.WaitDelay = time.Second
	if err := command.Run(); err != nil {
		if ctx.Err() != nil {
			return contexts.Selection{}, ctx.Err()
		}
		return contexts.Selection{}, state("selection account helper failed")
	}
	var response helperResponse
	if output.overflow || json.Unmarshal(output.data, &response) != nil || response.Failed {
		return contexts.Selection{}, state("selection account helper refused unsafe storage")
	}
	return response.Selection, nil
}

type boundedBuffer struct {
	data     []byte
	overflow bool
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	remaining := maximumRecord - len(b.data)
	if n > remaining {
		b.overflow = true
		p = p[:remaining]
	}
	b.data = append(b.data, p...)
	return n, nil
}

// ServeHelper accepts only the private selection protocol, under the account
// that will own the selection. It grants no path or root privilege to callers.
func ServeHelper(ctx context.Context, args []string, input io.Reader, output io.Writer) (bool, int) {
	if len(args) == 0 || args[0] != HelperMode {
		return false, 0
	}
	if len(args) != 1 || os.Getuid() != os.Geteuid() || os.Getuid() == 0 {
		return true, 1
	}
	data, err := io.ReadAll(io.LimitReader(input, maximumRecord+1))
	if err != nil || len(data) > maximumRecord {
		return true, 1
	}
	var request helperRequest
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&request) != nil || decoder.Decode(new(any)) != io.EOF || request.Account.UID != os.Getuid() || request.Account.GID != os.Getegid() || request.Account.Executable != "" {
		return true, 1
	}
	if request.Action != "read" && request.Action != "write" && request.Action != "clear" {
		return true, 1
	}
	selection, err := New(request.Account).local(ctx, request.Action, request.Selection)
	if json.NewEncoder(output).Encode(helperResponse{Selection: selection, Failed: err != nil}) != nil {
		return true, 1
	}
	return true, 0
}
