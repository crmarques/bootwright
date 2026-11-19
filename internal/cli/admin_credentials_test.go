package cli

import (
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func TestUpdateAdminCredsLoginListFollowsTheListRules(t *testing.T) {
	for _, test := range []struct {
		name, login string
		want        []string
		refusal     string
	}{
		{name: "default", want: nil},
		{name: "trimmed and deduplicated", login: " prometheus , grafana,prometheus", want: []string{"prometheus", "grafana"}},
		{name: "every login", login: "console,dashboard,prometheus,alertmanager,grafana", want: []string{"console", "dashboard", "prometheus", "alertmanager", "grafana"}},
		{name: "comma only", login: ",", refusal: "--login must select at least one value"},
		{name: "unknown member", login: "prometheus,loki", refusal: "--login accepts console, dashboard, prometheus, alertmanager, grafana"},
	} {
		t.Run(test.name, func(t *testing.T) {
			args := []string{"--context", "lab", "cluster", "update-admin-creds", "--name", "storage", "--yes"}
			if test.login != "" {
				args = append(args, "--login", test.login)
			}
			code, out, errOut, record := runRecorded(args)
			if test.refusal != "" {
				if code != 2 || record.calls != 0 || !strings.HasPrefix(errOut, "  error  cli.usage\n    "+test.refusal+"\n") {
					t.Fatalf("code=%d calls=%d out=%q stderr=%q", code, record.calls, out, errOut)
				}
				return
			}
			request, ok := record.request.(lifecycle.UpdateAdminCredentialsRequest)
			if record.calls != 1 || !ok || !slices.Equal(request.Logins, test.want) || request.Name != "storage" || !request.SkipConfirmation {
				t.Fatalf("code=%d calls=%d request=%+v stderr=%q", code, record.calls, record.request, errOut)
			}
		})
	}
}

func TestTheOldAdminCredentialsCommandIsUnknown(t *testing.T) {
	code, _, errOut, record := runRecorded([]string{"cluster", "update-admin-credentials", "--name", "storage", "--yes"})
	if code != 2 || record.calls != 0 || !strings.HasPrefix(errOut, "  error  cli.usage\n    unknown command: \"update-admin-credentials\"\n") {
		t.Fatalf("code=%d calls=%d stderr=%q", code, record.calls, errOut)
	}
}

func TestTheUpdateResultNamesOnlyThePublicAdministratorUsers(t *testing.T) {
	result := &lifecycle.UpdateAdminCredentialsResult{Context: "lab", Cluster: "storage", Logins: []lifecycle.AdminLoginResult{
		{Login: "dashboard", User: "admin"}, {Login: "prometheus", User: "prometheus-secret-user"}, {Login: "alertmanager", User: "alertmanager-secret-user"}, {Login: "grafana", User: "admin"}}}
	for _, structured := range []bool{false, true} {
		var out strings.Builder
		if err := writeAdminCredentials(&out, "cluster update-admin-creds", result, structured); err != nil {
			t.Fatal(err)
		}
		public := strings.Count(out.String(), `"user":"admin"`) == 2
		if !structured {
			public = strings.Contains(out.String(), "Dashboard user") && strings.Contains(out.String(), "Grafana user") && strings.Contains(out.String(), "Prometheus login")
		}
		if strings.Contains(out.String(), "secret-user") || !public {
			t.Fatalf("structured=%t printed %s", structured, out.String())
		}
	}
}
