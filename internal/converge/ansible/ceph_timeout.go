package ansible

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const mutatingInvocationExtraVar = "bootwright_mutating_invocation"

const cephCommandTimeoutMarker = "BOOTWRIGHT_CEPH_COMMAND_TIMEOUT="

type CephCommandTimeoutError struct {
	Err            error
	Task           string
	Host           string
	TimeoutSeconds string
	ExitCode       int
	Invocation     string
	StateChanging  bool
}

func (e *CephCommandTimeoutError) Error() string {
	if e.Err == nil {
		return e.OperatorMessage()
	}
	return fmt.Sprintf("%s: %v", e.OperatorMessage(), e.Err)
}

func (e *CephCommandTimeoutError) Unwrap() error {
	return e.Err
}

func (e *CephCommandTimeoutError) OperatorMessage() string {
	operation := "Ceph probe"
	if e.StateChanging {
		operation = "Ceph state-changing command"
	}
	target := ""
	if e.Task != "" {
		target = " " + strconv.Quote(e.Task)
	}
	if e.Host != "" {
		target += " on " + e.Host
	}
	duration := "its finite safety timeout"
	if e.TimeoutSeconds != "" {
		duration = "its " + e.TimeoutSeconds + "-second safety timeout"
	}
	message := fmt.Sprintf("%s%s exceeded %s and was terminated (rc %d). The outcome is unknown; Bootwright did not treat the timeout as evidence that the cluster is absent, safe to change, or successfully changed.", operation, target, duration, e.ExitCode)
	if !e.StateChanging {
		return message + " Repair the Ceph, quorum, network, registry, or host condition that kept the command from completing; consult the task log when it contains non-redacted details, then repeat the original read-only operation. No state-changing retry command is inferred from a failed probe."
	}
	if e.Invocation == "" {
		return message + " Repair the Ceph, quorum, network, registry, or host condition that kept the command from completing before retrying. The exact resolved invocation fact was unavailable, so Bootwright cannot safely invent a retry command."
	}
	return message + " Repair the Ceph, quorum, network, registry, or host condition that kept the command from completing; consult the task log when it contains non-redacted details, then retry the exact resolved invocation: `" + e.Invocation + "`."
}

type cephTimeoutEvidence struct {
	task           string
	host           string
	timeoutSeconds string
	exitCode       int
	stateChanging  bool
}

func cephCommandTimeoutFromLog(path string) (cephTimeoutEvidence, bool) {
	body, err := os.ReadFile(path)
	if err != nil {
		return cephTimeoutEvidence{}, false
	}
	task := ""
	var found cephTimeoutEvidence
	for _, raw := range strings.Split(string(body), "\n") {
		line := strings.TrimSpace(stripANSI(raw))
		if strings.HasPrefix(line, "TASK [") {
			task = ansibleTaskName(line)
			continue
		}
		if !strings.HasPrefix(line, "fatal:") && !strings.HasPrefix(line, "failed:") {
			continue
		}
		idx := strings.Index(line, "=> ")
		if idx < 0 {
			continue
		}
		var result any
		if err := json.Unmarshal([]byte(line[idx+len("=> "):]), &result); err != nil {
			continue
		}
		if evidence, ok := cephTimeoutMarkerResult(result); ok {
			evidence.host = ansibleResultHost(line)
			if evidence.task == "" {
				evidence.task = task
			}
			found = evidence
			continue
		}
		code, command, ok := cephTimeoutResult(result)
		if !ok {
			continue
		}
		found = cephTimeoutEvidence{
			task:           task,
			host:           ansibleResultHost(line),
			timeoutSeconds: timeoutOperand(command),
			exitCode:       code,
			stateChanging:  CephShellCommandStateChanging(command),
		}
	}
	return found, found.exitCode != 0
}

type cephTimeoutMarkerPayload struct {
	Task           string `json:"task"`
	TimeoutSeconds int    `json:"timeout_seconds"`
	ExitCode       int    `json:"exit_code"`
	StateChanging  bool   `json:"state_changing"`
}

func cephTimeoutMarkerResult(value any) (cephTimeoutEvidence, bool) {
	switch typed := value.(type) {
	case map[string]any:
		if message, ok := typed["msg"].(string); ok {
			message = strings.TrimSpace(message)
			if strings.HasPrefix(message, cephCommandTimeoutMarker) {
				encoded := strings.TrimSpace(strings.TrimPrefix(message, cephCommandTimeoutMarker))
				var payload cephTimeoutMarkerPayload
				if err := json.Unmarshal([]byte(encoded), &payload); err == nil &&
					(payload.ExitCode == 124 || payload.ExitCode == 137) && payload.TimeoutSeconds > 0 {
					return cephTimeoutEvidence{
						task:           strings.TrimSpace(payload.Task),
						timeoutSeconds: strconv.Itoa(payload.TimeoutSeconds),
						exitCode:       payload.ExitCode,
						stateChanging:  payload.StateChanging,
					}, true
				}
			}
		}
		if results, ok := typed["results"].([]any); ok {
			for _, result := range results {
				if evidence, ok := cephTimeoutMarkerResult(result); ok {
					return evidence, true
				}
			}
		}
	case []any:
		for _, result := range typed {
			if evidence, ok := cephTimeoutMarkerResult(result); ok {
				return evidence, true
			}
		}
	}
	return cephTimeoutEvidence{}, false
}

func ansibleTaskName(line string) string {
	start := strings.Index(line, "TASK [")
	if start < 0 {
		return ""
	}
	rest := line[start+len("TASK ["):]
	end := strings.Index(rest, "]")
	if end < 0 {
		return strings.TrimSpace(rest)
	}
	return strings.TrimSpace(rest[:end])
}

func cephTimeoutResult(value any) (int, []string, bool) {
	switch typed := value.(type) {
	case map[string]any:
		code, ok := resultExitCode(typed["rc"])
		if ok && (code == 124 || code == 137) {
			command := resultCommand(typed["cmd"])
			if cephadmShellIndex(command) >= 0 {
				return code, command, true
			}
		}
		if results, ok := typed["results"].([]any); ok {
			for _, result := range results {
				if code, command, ok := cephTimeoutResult(result); ok {
					return code, command, true
				}
			}
		}
	case []any:
		for _, result := range typed {
			if code, command, ok := cephTimeoutResult(result); ok {
				return code, command, true
			}
		}
	}
	return 0, nil, false
}

func resultExitCode(value any) (int, bool) {
	switch typed := value.(type) {
	case float64:
		return int(typed), typed == float64(int(typed))
	case json.Number:
		value, err := strconv.Atoi(string(typed))
		return value, err == nil
	case string:
		value, err := strconv.Atoi(strings.TrimSpace(typed))
		return value, err == nil
	case int:
		return typed, true
	default:
		return 0, false
	}
}

func resultCommand(value any) []string {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	command := make([]string, 0, len(items))
	for _, item := range items {
		text, ok := item.(string)
		if !ok {
			return nil
		}
		command = append(command, text)
	}
	return command
}

func timeoutOperand(command []string) string {
	if len(command) < 3 || filepath.Base(command[0]) != "timeout" {
		return ""
	}
	for i := 1; i < len(command); i++ {
		arg := command[i]
		switch {
		case arg == "--":
			if i+1 < len(command) {
				return command[i+1]
			}
			return ""
		case arg == "-k" || arg == "--kill-after" || arg == "-s" || arg == "--signal":
			i++
		case strings.HasPrefix(arg, "-"):
		default:
			return arg
		}
	}
	return ""
}

func cephadmShellIndex(command []string) int {
	for i := 0; i+1 < len(command); i++ {
		if filepath.Base(command[i]) == "cephadm" && command[i+1] == "shell" {
			return i
		}
	}
	return -1
}

func CephShellCommandStateChanging(command []string) bool {
	return !cephShellCommandReadOnly(command)
}

func cephShellCommandReadOnly(command []string) bool {
	shell := cephadmShellIndex(command)
	if shell < 0 {
		return false
	}
	separator := -1
	for i := shell + 2; i < len(command); i++ {
		if command[i] == "--" {
			separator = i
			break
		}
	}
	if separator < 0 || separator+1 >= len(command) {
		return false
	}
	child := command[separator+1:]
	if child[0] == "crushtool" {
		return true
	}
	if hasCommandExact(child, "bash", "-c", "command -v python3 >/dev/null 2>&1") {
		return true
	}
	if child[0] == "radosgw-admin" {
		return hasCommandPrefix(child[1:], "realm", "list") || hasCommandPrefix(child[1:], "zonegroup", "list") || hasCommandPrefix(child[1:], "zone", "list") || hasCommandPrefix(child[1:], "user", "info")
	}
	if child[0] != "ceph" {
		return false
	}
	args := make([]string, 0, len(child)-1)
	for _, arg := range child[1:] {
		if !strings.HasPrefix(arg, "--connect-timeout=") {
			args = append(args, arg)
		}
	}
	if len(args) == 0 {
		return false
	}
	if hasCommandExact(args, "health") || hasCommandExact(args, "health", "--format", "json") ||
		hasCommandExact(args, "fsid") || hasCommandExact(args, "status") ||
		hasCommandExact(args, "versions", "--format", "json") || hasCommandExact(args, "orch", "--help") {
		return true
	}
	for _, prefix := range [][]string{
		{"osd", "stat"}, {"osd", "tree"}, {"osd", "metadata"}, {"osd", "getcrushmap"},
		{"osd", "pool", "ls"}, {"osd", "crush", "rule", "dump"},
		{"osd", "erasure-code-profile", "ls"}, {"osd", "erasure-code-profile", "get"},
		{"fs", "ls"}, {"fs", "get"}, {"mon", "dump"}, {"mgr", "module", "ls"},
		{"mgr", "services"}, {"config", "dump"}, {"config", "get"}, {"config-key", "get"},
		{"orch", "ls"}, {"orch", "ps"}, {"orch", "host", "ls"}, {"orch", "device", "ls"},
		{"log", "last"}, {"cephadm", "get-user"},
	} {
		if hasCommandPrefix(args, prefix...) {
			return true
		}
	}
	return false
}

func hasCommandPrefix(command []string, prefix ...string) bool {
	if len(command) < len(prefix) {
		return false
	}
	for i := range prefix {
		if command[i] != prefix[i] {
			return false
		}
	}
	return true
}

func hasCommandExact(command []string, expected ...string) bool {
	return len(command) == len(expected) && hasCommandPrefix(command, expected...)
}

func exactMutatingInvocation(pairs []string) string {
	for i := len(pairs) - 1; i >= 0; i-- {
		var values map[string]any
		if err := json.Unmarshal([]byte(pairs[i]), &values); err != nil {
			continue
		}
		invocation, _ := values[mutatingInvocationExtraVar].(string)
		if invocation = strings.TrimSpace(invocation); invocation != "" {
			return invocation
		}
	}
	return ""
}
