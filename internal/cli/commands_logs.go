package cli

import (
	"context"
	"strconv"
	"strings"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation/logview"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func logsCommandSpec() commandSpec {
	return available(commandSpec{path: "logs", short: "Show a context's operation events and retained adapter output",
		flags: []flagSpec{
			stringFlag("operation", "Select an operation by its op- ID (default: the current operation)"),
			stringFlag("block", "Keep one block's events, or name the block whose output --raw prints"),
			boolFlag("raw", "Print the block's newest retained adapter output instead of events"),
			stringFlag("attempt", "Print attempt N's output with --raw (default: the newest)"),
			boolFlag("follow", "Stream new events and running blocks' output until the operation, run or controller install ends"),
			stringFlag("run", "Print a bounded run's output, named run-NNNNNN"),
			optionalValueFlag("install", logview.NewestInstall, "Print the newest controller install's output, or one named as --install=install-NNNNNN"),
		},
		long: "Render the events of a context's current operation, or of --operation, as lines oldest first: the operation log merged with every attempt log." +
			" --block keeps one block's events; --raw prints that block's newest retained adapter output instead, or attempt N's with --attempt." +
			" --follow prints what is written so far, then streams new events and each running block's output, prefixed by its block, until the operation leaves running." +
			" --run prints a bounded run's output and --install the host's newest controller install output, or one named as --install=install-NNNNNN; either follows with --follow." +
			" Text output escapes every line for the terminal; JSON and YAML carry the exact output bytes. --detail full prints each event's full UTC timestamp. It reads under short shared locks and never holds a lease an apply waits on."})
}

type LogsService interface {
	Show(context.Context, logview.Selection) (*logview.Result, error)
	Follow(context.Context, logview.Selection, logview.Sink) (*logview.Summary, error)
}

func (s Services) invokeLogs(ctx context.Context, values *requestValues) (commandResult, error) {
	if s.Logs == nil {
		return commandResult{}, errMissingService
	}
	selection := logview.Selection{
		Context: values.text("context"), Operation: values.text("operation"), Block: values.text("block"),
		Raw: values.boolean("raw"), Run: values.text("run"), Install: values.text("install"),
		FullTimes: stringValue(values.flags, "detail") == "full",
	}
	if attempt := values.text("attempt"); attempt != "" {
		selection.Attempt, _ = strconv.Atoi(attempt)
	}
	if values.boolean("follow") {
		summary, err := invokeResult(ctx, values, selection, func(ctx context.Context, selection logview.Selection) (*logview.Summary, error) {
			return s.Logs.Follow(ctx, selection, newLogsPresenter(s.logOutput))
		})
		return commandResult{logsFollow: summary}, err
	}
	result, err := invokeResult(ctx, values, selection, s.Logs.Show)
	return commandResult{logs: result}, err
}

func validateLogsFlags(command *cobra.Command) string {
	flags := command.Flags()
	selection := logview.Selection{Context: stringValue(flags, "context"), Operation: stringValue(flags, "operation"), Block: stringValue(flags, "block"),
		Raw: boolValue(flags, "raw"), Run: stringValue(flags, "run"), Install: stringValue(flags, "install")}
	if attempt := stringValue(flags, "attempt"); attempt != "" {
		selection.Attempt = -1
		if number, err := strconv.Atoi(attempt); err == nil && number > 0 && strconv.Itoa(number) == attempt {
			selection.Attempt = number
		}
	}
	if err := logview.Validate(selection); err != nil {
		if reported := diagnostics.Of(err); len(reported) == 1 {
			return reported[0].Message
		}
		return err.Error()
	}
	switch follow := boolValue(flags, "follow"); {
	case follow && selection.Raw:
		return "--follow streams events and output as they are written; --raw reads one whole output"
	case follow && selectedStructured(command):
		return "--follow streams lines and has no " + structuredLabel(command) + " form; drop --follow or --output"
	}
	return ""
}

func logsOperandRemedy(path string, flags *pflag.FlagSet) string {
	operands := flags.Args()
	if path != "logs" || len(operands) != 1 || stringValue(flags, "install") != logview.NewestInstall || !strings.HasPrefix(operands[0], "install-") ||
		logview.Validate(logview.Selection{Install: operands[0]}) != nil {
		return ""
	}
	return "--install takes its value after =; write --install=" + operands[0]
}

type logsHint struct {
	Context, Operation, Run, Install string
	Running                          bool
}

func logsCommand(hint logsHint) string {
	var command string
	switch {
	case hint.Install != "":
		command, hint.Context = "bootwright logs --install="+hint.Install, ""
	case !dnsLabel(hint.Context):
		return ""
	case hint.Run != "":
		command, hint.Operation = "bootwright logs --context "+hint.Context+" --run "+hint.Run, ""
	case hint.Running:
		command, hint.Operation = "bootwright logs --context "+hint.Context, ""
	case hint.Operation != "":
		command = "bootwright logs --context " + hint.Context + " --operation " + hint.Operation
	default:
		return ""
	}
	if logview.Validate(logview.Selection{Context: hint.Context, Operation: hint.Operation, Run: hint.Run, Install: hint.Install}) != nil {
		return ""
	}
	if hint.Running {
		command += " --follow"
	}
	return command
}

const followLabel = "Follow"

func logFields(location string, hint logsHint) []field {
	if location == "" {
		return nil
	}
	fields := []field{{Label: logLocationLabel, Value: location}}
	if command := logsCommand(hint); command != "" {
		fields = append(fields, field{Label: followLabel, Value: command})
	}
	return fields
}
