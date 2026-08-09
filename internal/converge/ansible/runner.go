package ansible

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/crmarques/bootwright/internal/host/callerio"
	"github.com/crmarques/bootwright/internal/host/localroot"
	"github.com/crmarques/bootwright/internal/host/ptyexec"
)

const processGroupTerminationGrace = 5 * time.Second

const OutputLogName = "ansible-output.log"

const TaskProfileName = "task-profile.jsonl"

const RunResultName = "run-result.jsonl"

const ProfileEnvVar = "BOOTWRIGHT_ANSIBLE_PROFILE"

const ArtifactsEnvVar = "BOOTWRIGHT_ANSIBLE_ARTIFACTS"

const profileCallbackName = "bw_profile"

const runResultCallbackName = "bw_run_result"

const profileCallbackRelPath = "ansible_collections/bootwright/core/plugins/callback"

const SystemTempDir = "/var/tmp"

const sshControlPersist = "300s"

const sshControlArgs = "-C -o ControlMaster=auto -o ControlPersist=" + sshControlPersist

const sshControlPathParent = "bootwright-cp"

const sshControlPathMaxLen = 90

const sshControlPathRootBase = "/run"

var systemTempEnv = []struct {
	key   string
	value string
}{
	{key: "ANSIBLE_LOCAL_TEMP", value: SystemTempDir},
	{key: "ANSIBLE_REMOTE_TEMP", value: SystemTempDir},
	{key: "ANSIBLE_REMOTE_TMP", value: SystemTempDir},
}

func joinTags(tags []string) string {
	out := make([]string, 0, len(tags))
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		out = append(out, tag)
	}
	return strings.Join(out, ",")
}

func cleanPathList(list string) string {
	parts := strings.Split(list, string(os.PathListSeparator))
	out := parts[:0]
	for _, p := range parts {
		if p == "" {
			continue
		}
		out = append(out, filepath.Clean(p))
	}
	return strings.Join(out, string(os.PathListSeparator))
}

func SystemTempEnv() map[string]string {
	out := map[string]string{}
	for _, item := range systemTempEnv {
		out[item.key] = item.value
	}
	return out
}

type RunSpec struct {
	Executable          string
	AnsibleCfg          string
	RolesPath           string
	CollectionsPath     string
	FilterPluginsPath   string
	Inventory           string
	Playbook            string
	Limit               string
	Forks               int
	ExtraVars           string
	ExtraVarPairs       []string
	Tags                []string
	SkipTags            []string
	ArtifactsDir        string
	OutputLogPath       string
	Check               bool
	AskBecomePass       bool
	BecomePasswordFile  string
	UseControllingTTY   bool
	ClassifyUnreachable bool
	ExtraEnv            map[string]string
}

type Runner interface {
	Run(context.Context, RunSpec) error
	Command(RunSpec) []string
}

type CommandRunner struct {
	Stdout io.Writer
	Stderr io.Writer
}

type UnreachableError struct {
	Err error
}

func (e *UnreachableError) Error() string {
	return e.Err.Error()
}

func (e *UnreachableError) Unwrap() error {
	return e.Err
}

func IsUnreachable(err error) bool {
	var unreachable *UnreachableError
	return errors.As(err, &unreachable)
}

func (r CommandRunner) Command(spec RunSpec) []string {
	executable := spec.Executable
	if executable == "" {
		executable = "ansible-playbook"
	}
	args := []string{
		executable,
		"-i", spec.Inventory,
		spec.Playbook,
		"-e", "@" + spec.ExtraVars,
	}
	for _, pair := range spec.ExtraVarPairs {
		args = append(args, "-e", pair)
	}
	if tags := joinTags(spec.Tags); tags != "" {
		args = append(args, "--tags", tags)
	}
	if skipTags := joinTags(spec.SkipTags); skipTags != "" {
		args = append(args, "--skip-tags", skipTags)
	}
	if spec.Limit != "" {
		args = append(args, "--limit", spec.Limit)
	}
	if spec.Forks > 0 {
		args = append(args, "-f", strconv.Itoa(spec.Forks))
	}
	if spec.Check {
		args = append(args, "--check")
	}
	if spec.BecomePasswordFile != "" {
		args = append(args, "--become-password-file", spec.BecomePasswordFile)
	} else if spec.AskBecomePass {
		args = append(args, "--ask-become-pass")
	}
	return args
}

func (r CommandRunner) Run(ctx context.Context, spec RunSpec) error {
	if err := os.MkdirAll(spec.ArtifactsDir, 0o700); err != nil {
		return fmt.Errorf("create Ansible artifacts directory: %w", err)
	}
	if err := os.Chmod(spec.ArtifactsDir, 0o700); err != nil {
		return fmt.Errorf("chmod Ansible artifacts directory: %w", err)
	}
	outputLogPath := spec.OutputLogPath
	if outputLogPath == "" {
		outputLogPath = filepath.Join(spec.ArtifactsDir, OutputLogName)
	}
	command := r.Command(spec)
	env := os.Environ()
	env = appendSystemTempEnv(env)
	env = append(env, "PYTHONUNBUFFERED=1")
	if spec.AnsibleCfg != "" {
		env = append(env, "ANSIBLE_CONFIG="+spec.AnsibleCfg)
	}
	if spec.RolesPath != "" {
		env = append(env, "ANSIBLE_ROLES_PATH="+cleanPathList(spec.RolesPath))
	}
	if spec.CollectionsPath != "" {
		env = append(env, "ANSIBLE_COLLECTIONS_PATH="+cleanPathList(spec.CollectionsPath))
	}
	if spec.FilterPluginsPath != "" {
		env = append(env, "ANSIBLE_FILTER_PLUGINS="+filepath.Clean(spec.FilterPluginsPath))
	}
	if spec.ArtifactsDir != "" {
		env = append(env, ArtifactsEnvVar+"="+filepath.Clean(spec.ArtifactsDir))
		if spec.ClassifyUnreachable {
			if err := os.Remove(filepath.Join(spec.ArtifactsDir, RunResultName)); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("reset Ansible run result: %w", err)
			}
		}
		env = appendCallbackEnv(env, spec.CollectionsPath, spec.ClassifyUnreachable)
	}
	if dir := sshControlPathForRun(); dir != "" {
		env = append(env, "ANSIBLE_SSH_CONTROL_PATH_DIR="+dir)
		env = appendSSHControlArgs(env)
	}
	if extra := sudoUserSitePackages(); extra != "" {
		env = appendPythonPath(env, extra)
	}
	env = appendExtraEnv(env, spec.ExtraEnv)
	err := RunLoggedCommand(ctx, command, env, outputLogPath, r.Stdout, r.Stderr, spec.UseControllingTTY)
	if err != nil && spec.ClassifyUnreachable && runResultIsUnreachable(filepath.Join(spec.ArtifactsDir, RunResultName)) {
		return &UnreachableError{Err: err}
	}
	return err
}

func RunLoggedCommand(ctx context.Context, command []string, env []string, outputLogPath string, stdout, stderr io.Writer, useControllingTTY bool) error {
	if len(command) == 0 {
		return errors.New("run logged command: empty command")
	}
	if err := os.MkdirAll(filepath.Dir(outputLogPath), 0o700); err != nil {
		return fmt.Errorf("create Ansible output log directory: %w", err)
	}
	if err := os.Chmod(filepath.Dir(outputLogPath), 0o700); err != nil {
		return fmt.Errorf("chmod Ansible output log directory: %w", err)
	}
	outputLog, err := os.OpenFile(outputLogPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("create Ansible output log: %w", err)
	}
	defer outputLog.Close()
	if err := os.Chmod(outputLogPath, 0o600); err != nil {
		return fmt.Errorf("chmod Ansible output log: %w", err)
	}
	lockedOutputLog := &lockedWriter{w: outputLog}

	workDir := stableWorkingDir()

	var runErr error
	if useControllingTTY {
		runErr = ptyexec.RunCommand(ctx, ttyOutputWriter(stdout, stderr, lockedOutputLog), nil, command, env, workDir)
	} else {
		cmd := exec.CommandContext(ctx, command[0], command[1:]...)
		cmd.Dir = workDir
		cmd.Stdout = teeWriter(stdout, lockedOutputLog)
		cmd.Stderr = teeWriter(stderr, lockedOutputLog)
		cmd.Stdin = os.Stdin
		cmd.Env = env
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		cmd.Cancel = func() error {
			if cmd.Process == nil {
				return nil
			}
			return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
		}
		cmd.WaitDelay = processGroupTerminationGrace
		runErr = cmd.Run()
		if ctx.Err() != nil && cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		if errors.Is(runErr, exec.ErrWaitDelay) && cmd.ProcessState != nil && cmd.ProcessState.Success() {
			runErr = nil
		}
	}
	if runErr != nil {
		_ = outputLog.Close()
		summary := summarizeFailure(outputLogPath, defaultFailureTailLines)
		if summary != "" {
			return fmt.Errorf("run %s exited with error (output log: %s):\n%s\n  underlying error: %w",
				command[0], outputLogPath, summary, runErr)
		}
		return fmt.Errorf("run %s (output log: %s): %w", command[0], outputLogPath, runErr)
	}
	return nil
}

func stableWorkingDir() string {
	for _, dir := range []string{SystemTempDir, "/"} {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			return dir
		}
	}
	return ""
}

func appendCallbackEnv(env []string, collectionsPath string, classifyUnreachable bool) []string {
	profile := strings.TrimSpace(os.Getenv(ProfileEnvVar)) != ""
	if !profile && !classifyUnreachable {
		return env
	}
	dir := profileCallbackDir(collectionsPath)
	if dir == "" {
		return env
	}
	callbacks := make([]string, 0, 2)
	if profile {
		callbacks = append(callbacks, profileCallbackName)
	}
	if classifyUnreachable {
		callbacks = append(callbacks, runResultCallbackName)
	}
	return append(env,
		"ANSIBLE_CALLBACK_PLUGINS="+dir,
		"ANSIBLE_CALLBACKS_ENABLED="+strings.Join(callbacks, ","),
	)
}

type runResultRecord struct {
	Status string `json:"status"`
}

func runResultIsUnreachable(path string) bool {
	body, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	sawUnreachable := false
	for _, line := range strings.Split(strings.TrimSpace(string(body)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var record runResultRecord
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			return false
		}
		switch record.Status {
		case "unreachable":
			sawUnreachable = true
		case "skipped":
		default:
			return false
		}
	}
	return sawUnreachable
}

func profileCallbackDir(collectionsPath string) string {
	for _, entry := range strings.Split(collectionsPath, string(os.PathListSeparator)) {
		if entry == "" {
			continue
		}
		dir := filepath.Join(filepath.Clean(entry), filepath.FromSlash(profileCallbackRelPath))
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			return dir
		}
	}
	return ""
}

func appendSSHControlArgs(env []string) []string {
	for _, entry := range env {
		if !strings.HasPrefix(entry, "ANSIBLE_SSH_ARGS=") {
			continue
		}
		if strings.TrimSpace(strings.TrimPrefix(entry, "ANSIBLE_SSH_ARGS=")) != "" {
			return env
		}
	}
	return append(env, "ANSIBLE_SSH_ARGS="+sshControlArgs)
}

var sshControlPath struct {
	mu      sync.Mutex
	created bool
	dir     string
}

func sshControlPathForRun() string {
	sshControlPath.mu.Lock()
	defer sshControlPath.mu.Unlock()
	if !sshControlPath.created {
		sshControlPath.dir = newSSHControlPathDir()
		sshControlPath.created = true
	}
	return sshControlPath.dir
}

func newSSHControlPathDir() string {
	base := sshControlPathBase()
	if base == "" {
		return ""
	}
	parent := filepath.Join(base, sshControlPathParent)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return ""
	}
	if !realDirOwnedByEUID(parent) {
		return ""
	}
	if err := os.Chmod(parent, 0o700); err != nil {
		return ""
	}
	reapAbandonedControlPaths(parent)
	dir := filepath.Join(parent, strconv.Itoa(os.Getpid()))
	if len(dir) > sshControlPathMaxLen {
		return ""
	}
	if err := os.RemoveAll(dir); err != nil {
		return ""
	}
	if err := os.Mkdir(dir, 0o700); err != nil {
		return ""
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		_ = os.RemoveAll(dir)
		return ""
	}
	return dir
}

func reapAbandonedControlPaths(parent string) {
	entries, err := os.ReadDir(parent)
	if err != nil {
		return
	}
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 0 || pid == os.Getpid() || processAlive(pid) {
			continue
		}
		_ = os.RemoveAll(filepath.Join(parent, entry.Name()))
	}
}

func processAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

func sshControlPathBase() string {
	if os.Geteuid() == 0 {
		if isExistingDir(sshControlPathRootBase) {
			return sshControlPathRootBase
		}
		return ""
	}
	if dir := strings.TrimSpace(os.Getenv("XDG_RUNTIME_DIR")); dir != "" && isExistingDir(dir) {
		return dir
	}
	if dir := filepath.Join(sshControlPathRootBase, "user", strconv.Itoa(os.Geteuid())); isExistingDir(dir) {
		return dir
	}
	return ""
}

func isExistingDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func realDirOwnedByEUID(path string) bool {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsDir() {
		return false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return false
	}
	return stat.Uid == uint32(os.Geteuid())
}

func appendExtraEnv(env []string, extra map[string]string) []string {
	if len(extra) == 0 {
		return env
	}
	keys := make([]string, 0, len(extra))
	for key := range extra {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		env = append(env, key+"="+extra[key])
	}
	return env
}

func appendSystemTempEnv(env []string) []string {
	for _, item := range systemTempEnv {
		env = append(env, item.key+"="+item.value)
	}
	return env
}

const defaultFailureTailLines = 50

func summarizeFailure(logPath string, tailLines int) string {
	body, err := os.ReadFile(logPath)
	if err != nil {
		return fmt.Sprintf("  (could not read output log %s: %v)", logPath, err)
	}
	text := string(body)
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if len(lines) == 0 || (len(lines) == 1 && lines[0] == "") {
		return fmt.Sprintf("  (output log %s is empty)", logPath)
	}
	failingTask, failureReason, failureHost := ansibleFailure(lines)
	start := len(lines) - tailLines
	if start < 0 {
		start = 0
	}
	tail := strings.Join(lines[start:], "\n")
	var b strings.Builder
	if failingTask != "" {
		b.WriteString("  failed task: " + failingTask + "\n")
	}
	if failureReason != "" {
		if failureHost != "" {
			b.WriteString("  failure: " + failureReason + " (host " + failureHost + ")\n")
		} else {
			b.WriteString("  failure: " + failureReason + "\n")
		}
	}
	if tail != "" {
		b.WriteString("  last " + strconv.Itoa(len(lines)-start) + " line(s) of output:\n")
		for _, line := range strings.Split(tail, "\n") {
			b.WriteString("    " + line + "\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

var ansiEscape = regexp.MustCompile("\x1b\\[[0-9;]*m")

func stripANSI(s string) string { return ansiEscape.ReplaceAllString(s, "") }

type ansibleFailureCandidate struct {
	task   string
	reason string
	host   string
}

func ansibleFailure(lines []string) (string, string, string) {
	task := ""
	lastTask := ""
	var failed, enriched, unreachable, rawFatal ansibleFailureCandidate
	for _, raw := range lines {
		line := strings.TrimSpace(stripANSI(raw))
		switch {
		case strings.HasPrefix(line, "TASK ["):
			task = line
			lastTask = line
		case strings.HasPrefix(line, "fatal:"):
			host := ansibleResultHost(line)
			if rawFatal.reason == "" {
				rawFatal = ansibleFailureCandidate{task: task, reason: line, host: host}
			}
			msg := ansibleResultMessage(line)
			switch {
			case msg == "":
			case strings.Contains(line, "UNREACHABLE!"):
				if unreachable.reason == "" {
					unreachable = ansibleFailureCandidate{task: task, reason: msg, host: host}
				}
			case failed.reason == "":
				failed = ansibleFailureCandidate{task: task, reason: msg, host: host}
			}
		case strings.HasPrefix(line, "[ERROR]:"):
			if enriched.reason == "" {
				enriched = ansibleFailureCandidate{task: task, reason: ansibleEnrichedMessage(line)}
			}
		}
	}
	for _, candidate := range []ansibleFailureCandidate{failed, enriched, unreachable, rawFatal} {
		if candidate.reason == "" {
			continue
		}
		if candidate.task == "" {
			return lastTask, candidate.reason, candidate.host
		}
		return candidate.task, candidate.reason, candidate.host
	}
	return lastTask, "", ""
}

func ansibleResultHost(line string) string {
	open := strings.Index(line, "[")
	if open < 0 {
		return ""
	}
	rest := line[open+1:]
	end := strings.Index(rest, "]")
	if end < 0 {
		return ""
	}
	return strings.TrimSpace(rest[:end])
}

func ansibleResultMessage(line string) string {
	idx := strings.Index(line, "=> ")
	if idx < 0 {
		return ""
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(line[idx+len("=> "):]), &result); err != nil {
		return ""
	}
	msg, _ := result["msg"].(string)
	return strings.Join(strings.Fields(msg), " ")
}

func ansibleEnrichedMessage(line string) string {
	msg := strings.TrimSpace(strings.TrimPrefix(line, "[ERROR]:"))
	for _, prefix := range []string{"Task failed:", "Action failed:"} {
		msg = strings.TrimSpace(strings.TrimPrefix(msg, prefix))
	}
	return strings.Join(strings.Fields(msg), " ")
}

func sudoUserSitePackages() string {
	if !localroot.IsInternalRootChild() {
		return ""
	}
	home, ok := localroot.CallerHomeDir()
	if !ok || home == "" {
		return ""
	}
	uid, _, ok := localroot.CallerUIDGID()
	if !ok {
		return ""
	}
	out, _, err := callerio.CommandOutput("python3", "-m", "site", "--user-site")
	if err != nil {
		return ""
	}
	site := strings.TrimSpace(string(out))
	if site == "" {
		return ""
	}
	cleanSite := filepath.Clean(site)
	cleanHome := filepath.Clean(home)
	if !strings.HasPrefix(cleanSite, cleanHome+string(os.PathSeparator)) && cleanSite != cleanHome {
		return ""
	}
	if info, err := os.Stat(site); err != nil || !info.IsDir() {
		return ""
	}
	if !callerOwnedChainTrusted(cleanSite, cleanHome, uid) {
		return ""
	}
	return site
}

func callerOwnedChainTrusted(path, home string, uid uint32) bool {
	for {
		info, err := os.Lstat(path)
		if err != nil {
			return false
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			return false
		}
		if stat.Uid != uid {
			return false
		}
		if info.Mode().Perm()&0o022 != 0 {
			return false
		}
		if path == home {
			return true
		}
		parent := filepath.Dir(path)
		if parent == path {
			return true
		}
		path = parent
	}
}

func appendPythonPath(env []string, extra string) []string {
	for index, entry := range env {
		if strings.HasPrefix(entry, "PYTHONPATH=") {
			existing := strings.TrimPrefix(entry, "PYTHONPATH=")
			env[index] = "PYTHONPATH=" + extra + string(os.PathListSeparator) + existing
			return env
		}
	}
	return append(env, "PYTHONPATH="+extra)
}

func teeWriter(primary io.Writer, log io.Writer) io.Writer {
	if primary == nil {
		return log
	}
	return io.MultiWriter(primary, log)
}

func ttyOutputWriter(stdout io.Writer, stderr io.Writer, log io.Writer) io.Writer {
	if stdout != nil {
		return teeWriter(stdout, log)
	}
	return teeWriter(stderr, log)
}

type lockedWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (w *lockedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.w.Write(p)
}
