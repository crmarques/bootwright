package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/converge"
	"github.com/crmarques/bootwright/internal/converge/bundle"
	"github.com/crmarques/bootwright/internal/host/callerio"
	"github.com/crmarques/bootwright/internal/render"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/state/desired"
	"github.com/crmarques/bootwright/internal/workspace"
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
	OK          bool         `json:"ok"`
	ExitCode    int          `json:"exitCode"`
	Command     []string     `json:"command,omitempty"`
	Error       string       `json:"error"`
	Diagnostics []Diagnostic `json:"diagnostics,omitempty"`
}

func writeCommandErrorJSON(stdout io.Writer, args []string, code int, err error) error {
	return output.JSON(stdout, commandErrorReport{
		OK:          false,
		ExitCode:    code,
		Command:     append([]string(nil), args...),
		Error:       err.Error(),
		Diagnostics: commandDiagnostics(err),
	})
}

func commandDiagnostics(err error) []Diagnostic {
	var validationErr desiredstate.ValidationError
	if errors.As(err, &validationErr) {
		return diagnosticsFromError(err)
	}
	diagnostics := diagnosticsFromError(err)
	remediation := commandErrorRemediation(err.Error())
	for i := range diagnostics {
		diagnostics[i].Remediation = remediation
		if diagnostics[i].Rule == "" || diagnostics[i].Rule == diagnostics[i].Message {
			diagnostics[i].Rule = err.Error()
		}
	}
	return diagnostics
}

func commandErrorRemediation(message string) string {
	switch {
	case strings.Contains(message, "legacy context registry map"):
		return "remove the legacy context registry and recreate contexts with bootwright context init <name> -f <path> --yes"
	case strings.Contains(message, "current context") && strings.Contains(message, "not available in shared storage"):
		return "run bootwright context list, then bootwright context use <name> or bootwright context init <name> -f <path> --yes"
	case strings.Contains(message, "context") && strings.Contains(message, "not ready"):
		return "run bootwright status and fix the reported checks"
	case strings.Contains(message, "would write OpenShift installer files with secret material"):
		return "rerun with --sensitive only for a local, unversioned output directory"
	case strings.Contains(message, "no such file or directory") || strings.Contains(message, "not found"):
		return "check that the referenced path or command exists and rerun the command"
	case strings.Contains(message, "unknown command") || strings.Contains(message, "invalid argument"):
		return "run bootwright --help or the parent command help to choose a supported command"
	default:
		return "fix the reported command error and rerun the command"
	}
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
	return workspace.BundleDir(bundleVersionMarker())
}

func prepareWorkflowBundle(skipExtract bool) (bundle.AnsibleBundleResult, error) {
	bundleDir, err := resolveBundleDir()
	if err != nil {
		return bundle.AnsibleBundleResult{}, err
	}
	return converge.PrepareWorkflowBundle(bundleDir, bundleVersionMarker(), skipExtract)
}

func prepareInitialBundle() (bundle.AnsibleBundleResult, bool, error) {
	bundleDir, err := resolveBundleDir()
	if err != nil {
		return bundle.AnsibleBundleResult{}, false, err
	}
	return converge.PrepareInitialBundle(bundleDir, bundleVersionMarker())
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
	if stderr == nil {
		return
	}
	if warning := secret.DirPermsWarning(secretsDir); warning != "" {
		output.New(stderr).Warning("secrets-dir permissions", warning)
	}
}

// strictSecretsDirCheck enforces 0700 on the secrets-dir and 0600 on
// every file inside.
func strictSecretsDirCheck(secretsDir string) *exitError {
	if err := secret.StrictDirCheck(secretsDir); err != nil {
		return failErr(1, err)
	}
	return nil
}
