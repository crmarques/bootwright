package cli

import (
	"context"
	"io"

	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

type SignOnService interface {
	Suspend(context.Context, lifecycle.SignOnRequest) (*lifecycle.SignOnResult, error)
	Resume(context.Context, lifecycle.SignOnRequest) (*lifecycle.SignOnResult, error)
}

func signOnRequest(values *requestValues) lifecycle.SignOnRequest {
	return lifecycle.SignOnRequest{ContextName: values.text("context"), Name: values.text("name"), SkipConfirmation: values.boolean("yes")}
}

func (s Services) invokeSignOnSuspend(ctx context.Context, values *requestValues, _ []string) (commandResult, error) {
	if s.SignOn == nil {
		return commandResult{}, errMissingService
	}
	result, err := invokeResult(ctx, values, signOnRequest(values), s.SignOn.Suspend)
	return commandResult{signOn: result}, err
}

func (s Services) invokeSignOnResume(ctx context.Context, values *requestValues, _ []string) (commandResult, error) {
	if s.SignOn == nil {
		return commandResult{}, errMissingService
	}
	result, err := invokeResult(ctx, values, signOnRequest(values), s.SignOn.Resume)
	return commandResult{signOn: result}, err
}

type signOnStatePresentation struct {
	State       string   `json:"state"`
	Provider    string   `json:"provider"`
	Changed     bool     `json:"changed"`
	LocalLogins []string `json:"localLogins"`
}

type signOnPresentation struct {
	Context string                  `json:"context"`
	Cluster string                  `json:"cluster"`
	SignOn  signOnStatePresentation `json:"signOn"`
}

func (signOnPresentation) documentedResult() {}

func writeSignOn(out io.Writer, command string, result *lifecycle.SignOnResult, structured bool) error {
	logins := make([]string, 0, len(result.LocalLogins))
	for _, login := range result.LocalLogins {
		logins = append(logins, escapeDisplayLine(login))
	}
	if structured {
		return writeEnvelope(out, commandEnvelope{SchemaVersion: "v1alpha1", Command: command, OK: true, ExitCode: 0,
			Result: signOnPresentation{Context: escapeDisplayLine(result.Context), Cluster: escapeDisplayLine(result.Cluster),
				SignOn: signOnStatePresentation{State: escapeDisplayLine(result.State), Provider: escapeDisplayLine(result.Provider), Changed: result.Changed, LocalLogins: logins}},
			Diagnostics: []diagnostic{}, Logs: []string{}})
	}
	var text display
	fields := []field{{Label: "Provider", Value: escapeDisplayLine(result.Provider)}}
	if result.State == lifecycle.SignOnSuspended {
		text.headline("OK", "Suspended single sign-on for "+result.Cluster+"; its management gateway signs users in locally")
		for _, login := range logins {
			fields = append(fields, field{Label: adminLoginTitle(login) + " login", Value: "local"})
		}
		fields = append(fields, field{Label: "Next", Value: "bootwright cluster info --context " + result.Context + " --name " + result.Cluster + " --secrets", Tone: "command"},
			field{Label: "Restore", Value: "bootwright cluster sign-on resume --context " + result.Context + " --name " + result.Cluster, Tone: "command"})
	} else {
		text.headline("OK", "Resumed single sign-on for "+result.Cluster+" through its identity provider")
	}
	if !result.Changed {
		fields = append(fields, field{Label: "Changed", Value: "nothing; the cluster was already in this state"})
	}
	text.mutedFields(fields...)
	return text.writeTo(out)
}
