//go:build linux && amd64

package selectionfs

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"time"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/nativeexec"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

const HelperMode = "__bootwright_selection"

type helperRequest struct {
	Account   Options            `json:"account"`
	Action    string             `json:"action"`
	Selection contexts.Selection `json:"selection"`
}

type helperResponse struct {
	Selection   contexts.Selection `json:"selection"`
	Failed      bool               `json:"failed"`
	Message     string             `json:"message,omitempty"`
	Remediation string             `json:"remediation,omitempty"`
}

const unclassifiedRefusal = "selection could not be accessed under its owning account"

// boundedText admits a short printable line, the only text the helper's
// response may carry into a diagnostic.
func boundedText(text string) bool {
	if text == "" || len(text) > 200 {
		return false
	}
	for _, r := range text {
		if r < ' ' || r > '~' {
			return false
		}
	}
	return true
}

// The helper is this same verified executable under the selection account, so
// its bounded state message is the accurate diagnosis of a refusal. Anything
// else reaching this decoder is reported as an unclassified refusal.
func refusal(message string) string {
	if !boundedText(message) {
		return unclassifiedRefusal
	}
	return message
}

func refusalOf(err error) (string, string) {
	if reported := diagnostics.Of(err); len(reported) == 1 {
		return reported[0].Message, reported[0].Remediation
	}
	return unclassifiedRefusal, ""
}

func (s *Store) perform(ctx context.Context, action string, selection contexts.Selection) (contexts.Selection, error) {
	if err := ctx.Err(); err != nil {
		return contexts.Selection{}, err
	}
	effective := os.Geteuid
	if s.effective != nil {
		effective = s.effective
	}
	euid := effective()
	if euid == s.options.UID {
		return s.local(ctx, action, selection)
	}
	if euid != 0 || s.options.UID <= 0 || s.options.GID < 0 {
		return contexts.Selection{}, state("selection account identity is unavailable")
	}
	options := s.options
	data, err := json.Marshal(helperRequest{Account: options, Action: action, Selection: selection})
	if err != nil || len(data) > maximumRecord {
		return contexts.Selection{}, state("selection helper request exceeds its limit")
	}
	operation, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	command := helperCommand(data, options)
	output := nativeexec.Head(maximumRecord)
	command.Stdout = output
	if result := s.launcher.Run(operation, command); !result.Succeeded() {
		if ctx.Err() != nil {
			return contexts.Selection{}, ctx.Err()
		}
		return contexts.Selection{}, state("selection account helper failed")
	}
	return decodeHelperResponse(output.Bytes(), output.Overflowed())
}

// decodeHelperResponse reads the helper's answer. A refusal keeps the
// account's own bounded diagnosis and its repair; an unclassified refusal
// never carries a repair.
func decodeHelperResponse(data []byte, overflow bool) (contexts.Selection, error) {
	var response helperResponse
	if overflow || json.Unmarshal(data, &response) != nil {
		return contexts.Selection{}, state("selection account helper returned no usable response")
	}
	if response.Failed {
		message := refusal(response.Message)
		if message != unclassifiedRefusal && boundedText(response.Remediation) {
			return contexts.Selection{}, contexts.StateErrorWithRemediation(message, response.Remediation)
		}
		return contexts.Selection{}, state(message)
	}
	return response.Selection, nil
}

// helperCommand runs this same program, pinned through /proc/self/exe so a
// file renamed over its path never runs as the selection account, in helper
// mode under that account, killed with its parent.
func helperCommand(data []byte, options Options) nativeexec.Command {
	command := nativeexec.Helper(HelperMode, nativeexec.Account{UID: uint32(options.UID), GID: uint32(options.GID), Groups: options.Groups})
	command.Stdin = bytes.NewReader(data)
	return command
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
		response.Message, response.Remediation = refusalOf(err)
	}
	if json.NewEncoder(output).Encode(response) != nil {
		return true, 1
	}
	return true, 0
}
