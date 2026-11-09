package logview

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/crmarques/bootwright/internal/availability"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

const (
	PollInterval   = time.Second
	MaxBusyPolls   = 60
	TailLines      = 10
	MaxLineBytes   = 16 << 10
	detachedPolls  = 2
	readChunkBytes = 64 << 10
)

type Options struct {
	Operations OperationStoreFactory
	Selection  CurrentSelection
	Pause      Pause
}

type Service struct {
	workspace Workspace
	options   Options
}

func New(workspace Workspace, options Options) Service {
	if options.Pause == nil {
		options.Pause = sleep
	}
	return Service{workspace: workspace, options: options}
}

func sleep(ctx context.Context, interval time.Duration) error {
	timer := time.NewTimer(interval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return context.Cause(ctx)
	case <-timer.C:
		return nil
	}
}

func (s Service) available(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.workspace == nil || s.options.Operations == nil || s.options.Pause == nil {
		return availability.ErrNotImplemented
	}
	return nil
}

func (s Service) resolve(ctx context.Context, name string) (string, error) {
	if name != "" {
		return name, nil
	}
	if s.options.Selection == nil {
		return "", diagnostics.NewFailure("context.state", "current context selection is not configured", "")
	}
	selected, err := s.options.Selection(ctx)
	if err != nil {
		return "", err
	}
	if selected == "" {
		return "", diagnostics.NewFailureWithRemediation("context.state", "no current context is selected",
			"", "select one with bootwright context use --name <context>, or name it with --context <context>")
	}
	return selected, nil
}

func (s Service) store(view View) OperationStore {
	return s.options.Operations(view.Records(), view.Logs())
}

func usage(message string) error {
	return &diagnostics.Failure{Usage: true, Diagnostics: []diagnostics.Diagnostic{{Severity: "error", Code: "cli.usage", Message: message}}}
}

func Validate(selection Selection) error { return validate(selection) }

func validate(selection Selection) error {
	stream := selection.Run != "" || selection.Install != ""
	switch {
	case selection.Run != "" && selection.Install != "":
		return usage("--run and --install select different outputs; name one")
	case stream && (selection.Operation != "" || selection.Block != "" || selection.Raw || selection.Attempt != 0):
		return usage("--run and --install select one whole output and take no --operation, --block, --raw or --attempt")
	case selection.Install != "" && selection.Context != "":
		return usage("--install reads the host's controller install output and takes no --context")
	case selection.Raw && selection.Block == "":
		return usage("--raw needs the --block whose output it reads")
	case selection.Attempt != 0 && !selection.Raw:
		return usage("--attempt selects the output --raw reads")
	case selection.Attempt < 0 || selection.Attempt > reconciliation.MaxAttempt:
		return usage("--attempt is a number from 1 to " + strconv.Itoa(reconciliation.MaxAttempt))
	case selection.Operation != "" && !reconciliation.ValidOperationID(selection.Operation):
		return usage("--operation names an operation as op- and 32 lowercase hexadecimal digits")
	case selection.Block != "" && !reconciliation.ValidSegment(selection.Block):
		return usage("--block names a block of the operation's plan")
	case selection.Run != "" && !validNumbered(selection.Run, "run-"):
		return usage("--run names a bounded run as run-NNNNNN")
	case selection.Install != "" && selection.Install != NewestInstall && !validNumbered(selection.Install, "install-") && !validInstallID(selection.Install):
		return usage("--install names a controller install as newest or install-NNNNNN")
	}
	return nil
}

func validNumbered(value, prefix string) bool {
	digits, found := strings.CutPrefix(value, prefix)
	_, err := reconciliation.ParseNumber(digits)
	return found && err == nil
}

func validInstallID(value string) bool {
	digits, found := strings.CutPrefix(value, "install-")
	return found && len(digits) == 32 && strings.Trim(digits, "0123456789abcdef") == ""
}

func notFound(message, remediation string) error {
	return diagnostics.NewFailureWithRemediation("logs.not-found", message, "", remediation)
}

func busy(err error) bool {
	for _, reported := range diagnostics.Of(err) {
		if reported.Code == "lifecycle.lease" {
			return true
		}
	}
	return false
}
