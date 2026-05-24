package ansible

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/crmarques/bootwright/internal/ptyexec"
)

const OutputLogName = "ansible-output.log"

// SystemTempDir is the controller and managed-host temp root used for Ansible.
const SystemTempDir = "/tmp"

var systemTempEnv = []struct {
	key   string
	value string
}{
	{key: "ANSIBLE_LOCAL_TEMP", value: SystemTempDir},
	{key: "ANSIBLE_REMOTE_TEMP", value: SystemTempDir},
	{key: "ANSIBLE_REMOTE_TMP", value: SystemTempDir},
}

// SystemTempEnv returns Ansible temp overrides that avoid controller and
// provider-host home directories.
func SystemTempEnv() map[string]string {
	out := map[string]string{}
	for _, item := range systemTempEnv {
		out[item.key] = item.value
	}
	return out
}

type RunSpec struct {
	Executable         string
	AnsibleCfg         string
	RolesPath          string
	CollectionsPath    string
	FilterPluginsPath  string
	Inventory          string
	Playbook           string
	Limit              string
	Forks              int
	ExtraVars          string
	ExtraVarPairs      []string
	ArtifactsDir       string
	Check              bool
	AskBecomePass      bool
	BecomePasswordFile string
	UseControllingTTY  bool
}

type Runner interface {
	Run(context.Context, RunSpec) error
	Command(RunSpec) []string
}

type CommandRunner struct {
	Stdout io.Writer
	Stderr io.Writer
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
	outputLogPath := filepath.Join(spec.ArtifactsDir, OutputLogName)
	outputLog, err := os.OpenFile(outputLogPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("create Ansible output log: %w", err)
	}
	defer outputLog.Close()
	if err := os.Chmod(outputLogPath, 0o600); err != nil {
		return fmt.Errorf("chmod Ansible output log: %w", err)
	}
	lockedOutputLog := &lockedWriter{w: outputLog}

	command := r.Command(spec)
	env := os.Environ()
	env = appendSystemTempEnv(env)
	if spec.AnsibleCfg != "" {
		env = append(env, "ANSIBLE_CONFIG="+spec.AnsibleCfg)
	}
	if spec.RolesPath != "" {
		env = append(env, "ANSIBLE_ROLES_PATH="+filepath.Clean(spec.RolesPath))
	}
	if spec.CollectionsPath != "" {
		env = append(env, "ANSIBLE_COLLECTIONS_PATH="+filepath.Clean(spec.CollectionsPath))
	}
	if spec.FilterPluginsPath != "" {
		env = append(env, "ANSIBLE_FILTER_PLUGINS="+filepath.Clean(spec.FilterPluginsPath))
	}
	if spec.ArtifactsDir != "" {
		env = append(env, "BOOTWRIGHT_ANSIBLE_ARTIFACTS="+filepath.Clean(spec.ArtifactsDir))
	}
	if extra := sudoUserSitePackages(); extra != "" {
		env = appendPythonPath(env, extra)
	}
	var runErr error
	if spec.UseControllingTTY {
		runErr = ptyexec.RunCommand(ctx, ttyOutputWriter(r.Stdout, r.Stderr, lockedOutputLog), nil, command, env)
	} else {
		cmd := exec.CommandContext(ctx, command[0], command[1:]...)
		cmd.Stdout = teeWriter(r.Stdout, lockedOutputLog)
		cmd.Stderr = teeWriter(r.Stderr, lockedOutputLog)
		cmd.Stdin = os.Stdin
		cmd.Env = env
		runErr = cmd.Run()
	}
	if runErr != nil {
		// Close the log so the failure summary reads a flushed file.
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

func appendSystemTempEnv(env []string) []string {
	for _, item := range systemTempEnv {
		env = append(env, item.key+"="+item.value)
	}
	return env
}

// defaultFailureTailLines is the number of trailing lines read from the
// Ansible output log on failure. Matches the architecture review's
// recommendation of ~50 lines as the sweet spot between context and noise.
const defaultFailureTailLines = 50

// summarizeFailure reads the trailing lines of the artifact log and
// extracts the failing task (last `TASK [...]` marker) and the first
// `fatal: ...` reason, plus the raw tail. If the log is unreadable
// the helper returns an explanatory line rather than the empty string
// — that way the user always sees *why* there is no tail, instead of
// the generic exec error.
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
	failingTask := ""
	failureReason := ""
	for _, line := range lines {
		if strings.HasPrefix(line, "TASK [") {
			failingTask = line
		}
		if strings.HasPrefix(line, "fatal:") && failureReason == "" {
			failureReason = line
		}
	}
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
		b.WriteString("  failure: " + failureReason + "\n")
	}
	if tail != "" {
		b.WriteString("  last " + strconv.Itoa(len(lines)-start) + " line(s) of output:\n")
		for _, line := range strings.Split(tail, "\n") {
			b.WriteString("    " + line + "\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func sudoUserSitePackages() string {
	sudoUser := os.Getenv("SUDO_USER")
	if sudoUser == "" || sudoUser == "root" {
		return ""
	}
	u, err := user.Lookup(sudoUser)
	if err != nil || u.HomeDir == "" {
		return ""
	}
	probe := exec.Command("python3", "-m", "site", "--user-site")
	probe.Env = append(os.Environ(), "HOME="+u.HomeDir)
	out, err := probe.Output()
	if err != nil {
		return ""
	}
	site := strings.TrimSpace(string(out))
	if site == "" {
		return ""
	}
	if info, err := os.Stat(site); err != nil || !info.IsDir() {
		return ""
	}
	return site
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
