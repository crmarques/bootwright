package cli

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/machine/sessionpolicy"
	"github.com/spf13/cobra"
)

func policyCommands() []commandSpec {
	return []commandSpec{
		available(commandSpec{path: "policy set", flags: []flagSpec{requiredPolicyFile(), boolFlag("check", "Validate and expand the policy without writing it")}, validate: validatePolicySet,
			results: []jsonResult{policySet{}}, render: renderPolicySet(),
			example: "--file sessions.yaml", invoke: Services.invokePolicySet, short: "Set a context's session access policy", long: "Validate and atomically replace the delegated SSH session policy of the selected context. Requires exactly one --file. --check runs the same validation and writes nothing. Either way the result lists every grant of the document with all its subjects and its expanded Machines."}),
		available(commandSpec{path: "policy show", results: []jsonResult{policyShown{}}, render: renderPolicyShow(),
			example: "--context lab", invoke: Services.invokePolicyShow, short: "Show a context's installed session access policy", long: "Print the installed SessionAccessPolicy document of the selected context exactly as stored, so the output can be saved and set again. A context with no installed policy says so and succeeds. Targets are not expanded."}),
		available(commandSpec{path: "policy delete", flags: []flagSpec{confirmationFlag()}, results: []jsonResult{policyDeletion{}}, render: renderPolicyDelete(),
			example: "--context lab", invoke: Services.invokePolicyDelete, short: "Delete a context's session access policy", long: "Remove the installed session access policy of the selected context. New delegated sessions are refused afterwards, as with an empty grants list; running sessions are unaffected. A context with no installed policy succeeds without a prompt. --yes skips ordinary confirmation."}),
		available(commandSpec{path: "policy permissions", results: []jsonResult{policyPermissions{}},
			render: renderPolicyPermissions(), example: "--context lab", invoke: Services.invokePolicyPermissions, short: "Show your delegated session grants"}),
	}
}

func requiredPolicyFile() flagSpec {
	flag := fileFlag("file", "Read one standalone SessionAccessPolicy YAML document")
	flag.required, flag.short, flag.kind = true, "f", "stringArray"
	return flag
}

func validatePolicySet(command *cobra.Command) string {
	if len(arrayValue(command.Flags(), "file")) > 1 {
		return "policy set requires exactly one --file"
	}
	return ""
}

type PolicyService interface {
	Set(ctx context.Context, contextName, file string, check bool) (string, []sessionpolicy.Grant, error)
	Show(context.Context, string) (string, *api.SessionAccessPolicy, []byte, error)
	Delete(ctx context.Context, contextName string, confirmed bool) (string, bool, error)
	Permissions(context.Context, string) (string, []sessionpolicy.Grant, error)
}

type policySet struct {
	Context string            `json:"context"`
	Checked bool              `json:"checked"`
	Grants  []policyGrantView `json:"grants"`
}

func (policySet) documentedResult() {}

type policyShown struct {
	Context   string              `json:"context"`
	Installed bool                `json:"installed"`
	Policy    *policyDocumentView `json:"policy,omitempty"`
}

func (policyShown) documentedResult() {}

type policyDeletion struct {
	Context string `json:"context"`
	Deleted bool   `json:"deleted"`
}

func (policyDeletion) documentedResult() {}

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

type policyDocumentView struct {
	APIVersion string             `json:"apiVersion"`
	Kind       string             `json:"kind"`
	Metadata   policyMetadataView `json:"metadata"`
	Spec       policySpecView     `json:"spec"`
}

type policyMetadataView struct {
	Name   string            `json:"name"`
	Labels map[string]string `json:"labels,omitempty"`
}

type policySpecView struct {
	Grants []policyAuthoredGrantView `json:"grants"`
}

type policyAuthoredGrantView struct {
	Name       string              `json:"name"`
	Subjects   []policySubjectView `json:"subjects"`
	Targets    policyTargetsView   `json:"targets"`
	RemoteUser string              `json:"remoteUser"`
	Exec       *policyExecView     `json:"exec,omitempty"`
	Rsh        *struct{}           `json:"rsh,omitempty"`
}

type policyTargetsView struct {
	MachineRefs []string `json:"machineRefs,omitempty"`
	ClusterRefs []string `json:"clusterRefs,omitempty"`
}

type policyExecView struct {
	Commands [][]string `json:"commands"`
}

func (s Services) invokePolicySet(ctx context.Context, values *requestValues, _ []string) (commandResult, error) {
	if s.Policy == nil {
		return commandResult{}, errMissingService
	}
	check := values.boolean("check")
	name, grants, err := s.Policy.Set(ctx, values.text("context"), values.singleFile(), check)
	if err != nil {
		return commandResult{}, err
	}
	return commandResult{policySet: &policySet{Context: name, Checked: check, Grants: policyGrantViews(grants)}}, nil
}

func (s Services) invokePolicyShow(ctx context.Context, values *requestValues, _ []string) (commandResult, error) {
	if s.Policy == nil {
		return commandResult{}, errMissingService
	}
	name, policy, document, err := s.Policy.Show(ctx, values.text("context"))
	if err != nil {
		return commandResult{}, err
	}
	shown := policyShown{Context: name, Installed: policy != nil}
	if policy != nil {
		shown.Policy = policyDocument(*policy)
	}
	return commandResult{policyShow: &shown, policyDocument: document}, nil
}

func (s Services) invokePolicyDelete(ctx context.Context, values *requestValues, _ []string) (commandResult, error) {
	if s.Policy == nil {
		return commandResult{}, errMissingService
	}
	name, deleted, err := s.Policy.Delete(ctx, values.text("context"), values.boolean("yes"))
	if err != nil {
		return commandResult{}, err
	}
	return commandResult{policyDelete: &policyDeletion{Context: name, Deleted: deleted}}, nil
}

func (s Services) invokePolicyPermissions(ctx context.Context, values *requestValues, _ []string) (commandResult, error) {
	if s.Policy == nil {
		return commandResult{}, errMissingService
	}
	name, grants, err := s.Policy.Permissions(ctx, values.text("context"))
	if err != nil {
		return commandResult{}, err
	}
	return commandResult{policyPermissions: &policyPermissions{Context: name, Grants: policyGrantViews(grants)}}, nil
}

func policyGrantViews(grants []sessionpolicy.Grant) []policyGrantView {
	views := make([]policyGrantView, 0, len(grants))
	for _, grant := range grants {
		views = append(views, policyGrantView{
			Name: grant.Name, Subjects: policySubjectViews(grant.Subjects), RemoteUser: grant.RemoteUser, MachineRefs: grant.MachineRefs, ClusterRefs: grant.ClusterRefs,
			Machines: grant.Machines, Commands: grant.Commands, Shell: grant.Shell,
		})
	}
	return views
}

func policySubjectViews(subjects []api.PolicySubject) []policySubjectView {
	views := make([]policySubjectView, 0, len(subjects))
	for _, subject := range subjects {
		views = append(views, policySubjectView{User: subject.User, Group: subject.Group})
	}
	return views
}

func policyDocument(policy api.SessionAccessPolicy) *policyDocumentView {
	grants := make([]policyAuthoredGrantView, 0, len(policy.Spec.Grants))
	for _, grant := range policy.Spec.Grants {
		view := policyAuthoredGrantView{
			Name: grant.Name, Subjects: policySubjectViews(grant.Subjects), RemoteUser: grant.RemoteUser, Rsh: grant.Rsh,
			Targets: policyTargetsView{MachineRefs: grant.Targets.MachineRefs, ClusterRefs: grant.Targets.ClusterRefs},
		}
		if grant.Exec != nil {
			view.Exec = &policyExecView{Commands: grant.Exec.Commands}
		}
		grants = append(grants, view)
	}
	return &policyDocumentView{
		APIVersion: policy.APIVersion, Kind: policy.Kind, Metadata: policyMetadataView{Name: policy.Metadata.Name, Labels: policy.Metadata.Labels},
		Spec: policySpecView{Grants: grants},
	}
}

func renderPolicySet() commandRenderer {
	return commandRenderer{
		structured: structuredShown(func(result commandResult) (jsonResult, []diagnostic, bool) {
			return result.policySet, nil, result.policySet != nil
		}),
		human: func(r *Runner, call renderCall) (bool, error) {
			result := call.result.policySet
			if result == nil {
				return false, nil
			}
			headline := "Session access policy set for " + result.Context
			if result.Checked {
				headline = "Session access policy for " + result.Context + " is valid; nothing was written"
			}
			_, err := fmt.Fprintf(r.config.Out, "%s\n%s", headline, policyGrantListing(result.Context, result.Grants))
			return true, err
		},
	}
}

func renderPolicyShow() commandRenderer {
	return commandRenderer{
		structured: structuredShown(func(result commandResult) (jsonResult, []diagnostic, bool) {
			return result.policyShow, nil, result.policyShow != nil
		}),
		human: func(r *Runner, call renderCall) (bool, error) {
			result := call.result.policyShow
			if result == nil {
				return false, nil
			}
			if !result.Installed {
				_, err := fmt.Fprintf(r.config.Out, "No session access policy is installed for %s\n", result.Context)
				return true, err
			}
			document := call.result.policyDocument
			if !bytes.HasSuffix(document, []byte("\n")) {
				document = append(slices.Clip(document), '\n')
			}
			_, err := r.config.Out.Write(document)
			return true, err
		},
	}
}

func renderPolicyDelete() commandRenderer {
	return commandRenderer{
		structured: structuredShown(func(result commandResult) (jsonResult, []diagnostic, bool) {
			return result.policyDelete, nil, result.policyDelete != nil
		}),
		human: func(r *Runner, call renderCall) (bool, error) {
			result := call.result.policyDelete
			if result == nil {
				return false, nil
			}
			line := "Session access policy deleted for " + result.Context
			if !result.Deleted {
				line = "No session access policy is installed for " + result.Context + "; nothing deleted"
			}
			_, err := fmt.Fprintln(r.config.Out, line)
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
			_, err := fmt.Fprintf(r.config.Out, "%s", policyGrantListing(result.Context, result.Grants))
			return true, err
		},
	}
}

func policyGrantListing(contextName string, grants []policyGrantView) string {
	var display strings.Builder
	if len(grants) == 0 {
		fmt.Fprintf(&display, "No delegated session grants in %s\n", contextName)
	}
	for _, grant := range grants {
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
	return display.String()
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
