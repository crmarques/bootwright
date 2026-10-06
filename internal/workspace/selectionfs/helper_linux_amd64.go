//go:build linux && amd64

package selectionfs

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/crmarques/bootwright/internal/diagnostics"
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
	Message   string             `json:"message,omitempty"`
}

const unclassifiedRefusal = "selection could not be accessed under its owning account"

// The helper is this same verified executable under the selection account, so
// its bounded state message is the accurate diagnosis of a refusal. Anything
// else reaching this decoder is reported as an unclassified refusal.
func refusal(message string) string {
	if message == "" || len(message) > 200 {
		return unclassifiedRefusal
	}
	for _, r := range message {
		if r < ' ' || r > '~' {
			return unclassifiedRefusal
		}
	}
	return message
}

func refusalMessage(err error) string {
	if reported := diagnostics.Of(err); len(reported) == 1 {
		return reported[0].Message
	}
	return unclassifiedRefusal
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
	options := s.options
	data, err := json.Marshal(helperRequest{Account: options, Action: action, Selection: selection})
	if err != nil || len(data) > maximumRecord {
		return contexts.Selection{}, state("selection helper request exceeds its limit")
	}
	operation, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	command := helperCommand(operation, data, options)
	var output boundedBuffer
	command.Stdout = &output
	command.Stderr = io.Discard
	if err := command.Run(); err != nil {
		if ctx.Err() != nil {
			return contexts.Selection{}, ctx.Err()
		}
		return contexts.Selection{}, state("selection account helper failed")
	}
	var response helperResponse
	if output.overflow || json.Unmarshal(output.data, &response) != nil {
		return contexts.Selection{}, state("selection account helper returned no usable response")
	}
	if response.Failed {
		return contexts.Selection{}, state(refusal(response.Message))
	}
	return response.Selection, nil
}

// helperExecutable is the running program's own image. The forked child
// resolves it to the image it was forked from after its credential drop,
// because a process may always read its own /proc/self/exe, so a file renamed
// over the program's path can never run as the selection account. The
// parent's /proc/<pid>/exe is not readable after that drop.
const helperExecutable = "/proc/self/exe"

// helperCommand runs this same program in helper mode under the selection
// account, with a fixed environment and directory, killed with its parent.
func helperCommand(ctx context.Context, data []byte, options Options) *exec.Cmd {
	command := exec.CommandContext(ctx, helperExecutable, HelperMode)
	command.Env = []string{"LANG=C", "LC_ALL=C"}
	command.Dir = "/"
	command.Stdin = bytes.NewReader(data)
	command.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: uint32(options.UID), Gid: uint32(options.GID), Groups: append([]uint32(nil), options.Groups...)}, Pdeathsig: syscall.SIGKILL}
	command.WaitDelay = time.Second
	return command
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
	if decoder.Decode(&request) != nil || decoder.Decode(new(any)) != io.EOF || request.Account.UID != os.Getuid() || request.Account.GID != os.Getegid() {
		return true, 1
	}
	if request.Action != "read" && request.Action != "write" && request.Action != "clear" {
		return true, 1
	}
	selection, err := New(request.Account).local(ctx, request.Action, request.Selection)
	response := helperResponse{Selection: selection, Failed: err != nil}
	if err != nil {
		response.Message = refusalMessage(err)
	}
	if json.NewEncoder(output).Encode(response) != nil {
		return true, 1
	}
	return true, 0
}
