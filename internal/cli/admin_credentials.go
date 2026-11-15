package cli

import (
	"context"
	"io"

	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

type AdminCredentialsService interface {
	UpdateAdminCredentials(context.Context, lifecycle.UpdateAdminCredentialsRequest) (*lifecycle.UpdateAdminCredentialsResult, error)
}

func (s Services) invokeAdminCredentials(ctx context.Context, values *requestValues) (commandResult, error) {
	if s.AdminCredentials == nil {
		return commandResult{}, errMissingService
	}
	result, err := invokeResult(ctx, values, lifecycle.UpdateAdminCredentialsRequest{ContextName: values.text("context"), Name: values.text("name"), SkipConfirmation: values.boolean("yes")}, s.AdminCredentials.UpdateAdminCredentials)
	return commandResult{adminCredentials: result}, err
}

type adminCredentialsPresentation struct {
	Context string `json:"context"`
	Cluster string `json:"cluster"`
	User    string `json:"user"`
}

func (adminCredentialsPresentation) documentedResult() {}

func writeAdminCredentials(out io.Writer, command string, result *lifecycle.UpdateAdminCredentialsResult, structured bool) error {
	if structured {
		return writeEnvelope(out, commandEnvelope{SchemaVersion: "v1alpha1", Command: command, OK: true, ExitCode: 0,
			Result: adminCredentialsPresentation{Context: escapeDisplayLine(result.Context), Cluster: escapeDisplayLine(result.Cluster), User: escapeDisplayLine(result.User)}, Diagnostics: []diagnostic{}, Logs: []string{}})
	}
	var text display
	text.headline("OK", "Updated administrator credentials for "+result.Cluster)
	text.mutedFields(field{Label: "User", Value: result.User}, field{Label: "Next", Value: "bootwright cluster info --context " + result.Context + " --name " + result.Cluster + " --secrets"})
	return text.writeTo(out)
}
