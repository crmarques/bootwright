package cli

import (
	"context"
	"io"

	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

type AdminCredentialsService interface {
	UpdateAdminCredentials(context.Context, lifecycle.UpdateAdminCredentialsRequest) (*lifecycle.UpdateAdminCredentialsResult, error)
}

func (s Services) invokeAdminCredentials(ctx context.Context, values *requestValues, _ []string) (commandResult, error) {
	if s.AdminCredentials == nil {
		return commandResult{}, errMissingService
	}
	request := lifecycle.UpdateAdminCredentialsRequest{ContextName: values.text("context"), Name: values.text("name"), Logins: values.names("login"), SkipConfirmation: values.boolean("yes")}
	result, err := invokeResult(ctx, values, request, s.AdminCredentials.UpdateAdminCredentials)
	return commandResult{adminCredentials: result}, err
}

type adminLoginPresentation struct {
	Login string `json:"login"`
	User  string `json:"user,omitempty"`
}

type adminCredentialsPresentation struct {
	Context string                   `json:"context"`
	Cluster string                   `json:"cluster"`
	Logins  []adminLoginPresentation `json:"logins"`
}

func (adminCredentialsPresentation) documentedResult() {}

func writeAdminCredentials(out io.Writer, command string, result *lifecycle.UpdateAdminCredentialsResult, structured bool) error {
	logins := make([]adminLoginPresentation, 0, len(result.Logins))
	for _, login := range result.Logins {
		logins = append(logins, adminLoginPresentation{Login: escapeDisplayLine(login.Login), User: escapeDisplayLine(publicAdminUser(login))})
	}
	if structured {
		return writeEnvelope(out, commandEnvelope{SchemaVersion: "v1alpha1", Command: command, OK: true, ExitCode: 0,
			Result: adminCredentialsPresentation{Context: escapeDisplayLine(result.Context), Cluster: escapeDisplayLine(result.Cluster), Logins: logins}, Diagnostics: []diagnostic{}, Logs: []string{}})
	}
	var text display
	text.headline("OK", "Updated administrator credentials for "+result.Cluster)
	fields := make([]field, 0, len(result.Logins)+1)
	for _, login := range result.Logins {
		if user := publicAdminUser(login); user != "" {
			fields = append(fields, field{Label: adminLoginTitle(login.Login) + " user", Value: user})
			continue
		}
		fields = append(fields, field{Label: adminLoginTitle(login.Login) + " login", Value: "updated"})
	}
	fields = append(fields, field{Label: "Next", Value: "bootwright cluster info --context " + result.Context + " --name " + result.Cluster + " --secrets", Tone: "command"})
	text.mutedFields(fields...)
	return text.writeTo(out)
}

func publicAdminUser(login lifecycle.AdminLoginResult) string {
	if login.Login == "prometheus" || login.Login == "alertmanager" {
		return ""
	}
	return login.User
}

func adminLoginTitle(login string) string {
	switch login {
	case "console":
		return "Console"
	case "dashboard":
		return "Dashboard"
	case "prometheus":
		return "Prometheus"
	case "alertmanager":
		return "Alertmanager"
	case "grafana":
		return "Grafana"
	}
	return login
}
