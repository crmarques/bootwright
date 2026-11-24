package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/crmarques/bootwright/internal/machine/sessionpolicy"
	"github.com/spf13/cobra"
)

func policyCommands() []commandSpec {
	return []commandSpec{
		available(commandSpec{path: "policy install", flags: []flagSpec{requiredPolicyFile()}, validate: validatePolicyInstall,
			results: []jsonResult{policyInstallation{}}, render: renderPolicyInstall(),
			example: "--file sessions.yaml", invoke: Services.invokePolicyInstall, short: "Install a context's session access policy", long: "Validate and atomically replace the delegated SSH session policy of the selected context. Requires exactly one --file."}),
		available(commandSpec{path: "policy permissions", results: []jsonResult{policyPermissions{}},
			render: renderPolicyPermissions(), example: "--context lab", invoke: Services.invokePolicyPermissions, short: "Show your delegated session grants"}),
	}
}

func requiredPolicyFile() flagSpec {
	flag := fileFlag("file", "Read one standalone SessionAccessPolicy YAML document")
	flag.required, flag.short, flag.kind = true, "f", "stringArray"
	return flag
}

func validatePolicyInstall(command *cobra.Command) string {
	if len(arrayValue(command.Flags(), "file")) > 1 {
		return "policy install requires exactly one --file"
	}
	return ""
}

type PolicyService interface {
	Install(context.Context, string, string) (string, error)
	Permissions(context.Context, string) (string, []sessionpolicy.Grant, error)
}

type policyInstallation struct {
	Context string `json:"context"`
}

func (policyInstallation) documentedResult() {}

type policyPermissions struct {
	Context string            `json:"context"`
	Grants  []policyGrantView `json:"grants"`
}

func (policyPermissions) documentedResult() {}

type policyGrantView struct {
	Name        string              `json:"name"`
	Subjects    []policySubjectView `json:"subjects"`
	RemoteUser  string              `json:"remoteUser"`
	MachineRefs []string            `json:"machineRefs,omitempty"`
	ClusterRefs []string            `json:"clusterRefs,omitempty"`
	Machines    []string            `json:"machines"`
	Commands    [][]string          `json:"commands,omitempty"`
	Shell       bool                `json:"shell"`
}

type policySubjectView struct {
	User  string `json:"user,omitempty"`
	Group string `json:"group,omitempty"`
}

func (s Services) invokePolicyInstall(ctx context.Context, values *requestValues, _ []string) (commandResult, error) {
	if s.Policy == nil {
		return commandResult{}, errMissingService
	}
	name, err := s.Policy.Install(ctx, values.text("context"), values.singleFile())
	if err != nil {
		return commandResult{}, err
	}
	return commandResult{policyInstall: &policyInstallation{Context: name}}, nil
}

func (s Services) invokePolicyPermissions(ctx context.Context, values *requestValues, _ []string) (commandResult, error) {
	if s.Policy == nil {
		return commandResult{}, errMissingService
	}
	name, grants, err := s.Policy.Permissions(ctx, values.text("context"))
	if err != nil {
		return commandResult{}, err
	}
	result := policyPermissions{Context: name, Grants: make([]policyGrantView, 0, len(grants))}
	for _, grant := range grants {
		subjects := make([]policySubjectView, 0, len(grant.Subjects))
		for _, subject := range grant.Subjects {
			subjects = append(subjects, policySubjectView{User: subject.User, Group: subject.Group})
		}
		result.Grants = append(result.Grants, policyGrantView{
			Name: grant.Name, Subjects: subjects, RemoteUser: grant.RemoteUser, MachineRefs: grant.MachineRefs, ClusterRefs: grant.ClusterRefs,
			Machines: grant.Machines, Commands: grant.Commands, Shell: grant.Shell,
		})
	}
	return commandResult{policyPermissions: &result}, nil
}

func renderPolicyInstall() commandRenderer {
	return commandRenderer{
		structured: structuredShown(func(result commandResult) (jsonResult, []diagnostic, bool) {
			return result.policyInstall, nil, result.policyInstall != nil
		}),
		human: func(r *Runner, call renderCall) (bool, error) {
			if call.result.policyInstall == nil {
				return false, nil
			}
			_, err := fmt.Fprintf(r.config.Out, "Session access policy installed for %s\n", call.result.policyInstall.Context)
			return true, err
		},
	}
}

func renderPolicyPermissions() commandRenderer {
	return commandRenderer{
		structured: structuredShown(func(result commandResult) (jsonResult, []diagnostic, bool) {
			return result.policyPermissions, nil, result.policyPermissions != nil
		}),
		human: func(r *Runner, call renderCall) (bool, error) {
			result := call.result.policyPermissions
			if result == nil {
				return false, nil
			}
			var display strings.Builder
			if len(result.Grants) == 0 {
				fmt.Fprintf(&display, "No delegated session grants in %s\n", result.Context)
			}
			for _, grant := range result.Grants {
				fmt.Fprintf(&display, "%s (as %s)\n", grant.Name, grant.RemoteUser)
				fmt.Fprintf(&display, "  Granted to: %s\n", policySubjects(grant.Subjects))
				if len(grant.ClusterRefs) > 0 {
					fmt.Fprintf(&display, "  Cluster selectors: %s\n", strings.Join(grant.ClusterRefs, ", "))
				}
				if len(grant.MachineRefs) > 0 {
					fmt.Fprintf(&display, "  Machine selectors: %s\n", strings.Join(grant.MachineRefs, ", "))
				}
				fmt.Fprintf(&display, "  Effective Machines: %s\n", strings.Join(grant.Machines, ", "))
				if grant.Shell {
					display.WriteString("  Interactive shell\n")
				}
				for _, argv := range grant.Commands {
					fmt.Fprintf(&display, "  Exec %q\n", argv)
				}
			}
			_, err := fmt.Fprintf(r.config.Out, "%s", display.String())
			return true, err
		},
	}
}

func policySubjects(subjects []policySubjectView) string {
	names := make([]string, 0, len(subjects))
	for _, subject := range subjects {
		if subject.Group != "" {
			names = append(names, "group "+subject.Group)
			continue
		}
		names = append(names, "user "+subject.User)
	}
	return strings.Join(names, ", ")
}
