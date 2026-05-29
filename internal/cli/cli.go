package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/converge/bundle"
	"github.com/crmarques/bootwright/internal/render"
	"github.com/crmarques/bootwright/internal/runtime/root/callerio"
	"github.com/crmarques/bootwright/internal/state/desired"
)

func Run(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) int {
	if code, handled := callerio.RunHelper(args, stdout, stderr); handled {
		return code
	}
	if code, handled, err := ensureLocalRootForArgs(ctx, args, stdin, stdout, stderr); handled {
		return code
	} else if err != nil {
		if argsRequestJSON(args) {
			_ = writeCommandErrorJSON(stdout, args, 1, err)
			return 1
		}
		output.New(stderr).Error(err)
		return 1
	}
	root := newRootCmd(stdin, stdout, stderr)
	root.SetArgs(args)
	err := root.ExecuteContext(ctx)
	if err == nil {
		return 0
	}
	var ee *exitError
	if errors.As(err, &ee) {
		if !ee.silent && ee.err != nil {
			if argsRequestJSON(args) {
				_ = writeCommandErrorJSON(stdout, args, ee.code, ee.err)
				return ee.code
			}
			output.New(stderr).Error(ee.err)
		}
		return ee.code
	}
	if argsRequestJSON(args) {
		_ = writeCommandErrorJSON(stdout, args, 1, err)
		return 1
	}
	output.New(stderr).Error(err)
	return 1
}

type commandErrorReport struct {
	OK          bool                      `json:"ok"`
	ExitCode    int                       `json:"exitCode"`
	Command     []string                  `json:"command,omitempty"`
	Error       string                    `json:"error"`
	Diagnostics []desiredstate.Diagnostic `json:"diagnostics,omitempty"`
}

func writeCommandErrorJSON(stdout io.Writer, args []string, code int, err error) error {
	return output.JSON(stdout, commandErrorReport{
		OK:          false,
		ExitCode:    code,
		Command:     append([]string(nil), args...),
		Error:       err.Error(),
		Diagnostics: desiredstate.Diagnostics(err),
	})
}

func argsRequestJSON(args []string) bool {
	for i, arg := range args {
		if arg == "--output" && i+1 < len(args) && args[i+1] == outputJSON {
			return true
		}
		if arg == "--output="+outputJSON {
			return true
		}
	}
	return false
}

type exitError struct {
	code   int
	err    error
	silent bool
}

func (e *exitError) Error() string {
	if e == nil || e.err == nil {
		return ""
	}
	return e.err.Error()
}

func (e *exitError) Unwrap() error { return e.err }

func failErr(code int, err error) *exitError { return &exitError{code: code, err: err} }
func failf(code int, format string, a ...any) *exitError {
	return &exitError{code: code, err: fmt.Errorf(format, a...)}
}
func silentExit(code int) *exitError { return &exitError{code: code, silent: true} }

func resolveBundleDir() (string, error) {
	return filepath.Abs(filepath.Join(cacheDir(), ansibleBundlesDirName, bundleVersionMarker()))
}

func prepareWorkflowBundle(skipExtract bool) (bundle.AnsibleBundleResult, error) {
	bundleDir, err := resolveBundleDir()
	if err != nil {
		return bundle.AnsibleBundleResult{}, err
	}
	if skipExtract {
		return bundle.AnsibleBundleResult{Dir: bundleDir, Reused: true}, nil
	}
	return bundle.EnsureAnsibleBundle(bundleDir, bundleVersionMarker())
}

func prepareInitialBundle() (bundle.AnsibleBundleResult, bool, error) {
	result, err := prepareWorkflowBundle(false)
	if err == nil {
		return result, false, nil
	}
	if !bundle.IsEmptyAnsibleBundle(err) {
		return bundle.AnsibleBundleResult{}, false, err
	}
	bundleDir, dirErr := resolveBundleDir()
	if dirErr != nil {
		return bundle.AnsibleBundleResult{}, false, dirErr
	}
	return bundle.AnsibleBundleResult{Dir: bundleDir, Reused: true}, true, nil
}

func printRenderResult(stdout io.Writer, result render.Result) {
	groups := []output.ArtifactGroup{{
		Name: "State and Ansible",
		Paths: []string{
			result.EffectiveStatePath,
			result.LockPath,
			result.InventoryPath,
			result.VarsPath,
		},
	}}
	var installerPaths []string
	for _, asset := range result.InstallerAssets {
		installerPaths = append(installerPaths, asset.InstallConfigPath, asset.AgentConfigPath)
	}
	if len(installerPaths) > 0 {
		groups = append(groups, output.ArtifactGroup{Name: "Installer placeholders", Paths: installerPaths})
	}
	p := output.NewContinuation(stdout)
	p.Section("Rendered artifacts")
	p.Artifacts(groups)
}

// warnSecretsDirPerms emits a one-line stderr warning when the local
// secrets directory exists with permission bits other than 0700.
func warnSecretsDirPerms(secretsDir string, stderr io.Writer) {
	if secretsDir == "" || stderr == nil {
		return
	}
	info, err := os.Stat(secretsDir)
	if err != nil || !info.IsDir() {
		return
	}
	mode := info.Mode().Perm()
	if mode == 0o700 {
		return
	}
	output.New(stderr).Warning("secrets-dir permissions", fmt.Sprintf("%s has mode %#o; expected 0700 (run chmod 0700 %s)", secretsDir, mode, secretsDir))
}

// strictSecretsDirCheck enforces 0700 on the secrets-dir and 0600 on
// every file inside.
func strictSecretsDirCheck(secretsDir string) *exitError {
	if secretsDir == "" {
		return nil
	}
	info, err := os.Stat(secretsDir)
	if err != nil {
		return failf(1, "strict-secrets: cannot stat secrets-dir %s: %v", secretsDir, err)
	}
	if !info.IsDir() {
		return failf(1, "strict-secrets: secrets-dir %s is not a directory", secretsDir)
	}
	if mode := info.Mode().Perm(); mode != 0o700 {
		return failf(1, "strict-secrets: secrets-dir %s has mode %#o; required 0700", secretsDir, mode)
	}
	entries, err := os.ReadDir(secretsDir)
	if err != nil {
		return failf(1, "strict-secrets: cannot read secrets-dir %s: %v", secretsDir, err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		fi, err := entry.Info()
		if err != nil {
			return failf(1, "strict-secrets: cannot stat %s/%s: %v", secretsDir, entry.Name(), err)
		}
		if mode := fi.Mode().Perm(); mode != 0o600 {
			return failf(1, "strict-secrets: %s/%s has mode %#o; required 0600", secretsDir, entry.Name(), mode)
		}
	}
	return nil
}
