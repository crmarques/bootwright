package packages

import (
	"net/url"
	"strings"
	"time"
	"unicode"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/addons"
	co "github.com/crmarques/bootwright/internal/addons/clusterobjects"
)

func gitops() addons.Package {
	return addons.Package{Name: "gitops", Summary: "OpenShift GitOps (Argo CD) with an optional bootstrap Application, installed last", Last: true, PlacementNamespaces: []string{"openshift-gitops"}, Budget: 20 * time.Minute, Normalize: normalizeGitOps, OLM: &addons.OLM{Namespace: "openshift-gitops-operator", NamespaceLabels: map[string]string{"openshift.io/cluster-monitoring": "true"}, OperatorGroup: addons.AllNamespaces, GroupName: "openshift-gitops-operator", Subscription: "openshift-gitops-operator", Package: "openshift-gitops-operator", DefaultSource: "redhat-operators", Approval: addons.Exact}, Admit: admitGitOps, Render: renderGitOps}
}

func normalizeGitOps(v api.Value) api.Value {
	b := v.Get("bootstrap")
	if !b.Present() {
		return v
	}
	sync := b.Get("sync").Default("automated", api.BoolValue(true)).Default("prune", api.BoolValue(false)).Default("selfHeal", api.BoolValue(false))
	return v.With("bootstrap", b.With("sync", sync))
}

func admitGitOps(i addons.Instance, _ addons.ClusterFacts) []api.Issue {
	b := i.Config.Get("bootstrap")
	if !b.Present() {
		return nil
	}
	issues := []api.Issue{}
	add := func(code, field, message, remedy string) {
		issues = append(issues, api.Issue{Code: code, Field: ".bootstrap." + field, Message: message, Remediation: remedy})
	}
	u, err := url.Parse(b.Get("repoURL").Text())
	if err == nil && u.User != nil {
		add("api.field", "repoURL", "bootstrap repoURL of package gitops carries user information", "remove the user information and name a usernamePassword Secret in bootstrap.credentialsRef")
	} else if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.Contains(b.Get("repoURL").Text(), "#") {
		add("api.field", "repoURL", "bootstrap repoURL of package gitops is not an https URL with a host and no query or fragment", "set the https URL of the repository")
	}
	path := b.Get("path").Text()
	bad := strings.HasPrefix(path, "/")
	for _, s := range strings.Split(path, "/") {
		bad = bad || s == ".."
	}
	if bad {
		add("api.field", "path", "bootstrap path "+path+" of package gitops leaves the repository", "set a relative path without .. segments")
	}
	revision := b.Get("targetRevision").Text()
	if revision == "" || strings.IndexFunc(revision, unicode.IsSpace) >= 0 {
		add("api.field", "targetRevision", "bootstrap targetRevision of package gitops must be non-empty without whitespace", "set a branch, tag or commit without whitespace")
	}
	sync := b.Get("sync")
	if !sync.Get("automated").Bool() && (sync.Get("prune").Bool() || sync.Get("selfHeal").Bool()) {
		add("api.invariant", "sync", "bootstrap sync prune and selfHeal of package gitops need sync.automated", "set sync.automated to true, or remove prune and selfHeal")
	}
	return issues
}

func renderGitOps(i addons.Instance) (addons.Rendered, error) {
	c, err := addons.Decode[api.GitOpsConfig](i.Config)
	if err != nil {
		return addons.Rendered{}, err
	}
	argo := co.Ref{APIVersion: "argoproj.io/v1beta1", Kind: "ArgoCD", Namespace: "openshift-gitops", Name: "openshift-gitops", Namespaced: true}
	result := addons.Rendered{Checks: []co.Check{phaseCheck(argo, "status.server", "Running"), phaseCheck(argo, "status.phase", "Available")}}
	b := c.Bootstrap
	if b == nil {
		return result, nil
	}
	phase := co.Phase{Gate: []co.Check{crd("applications.argoproj.io"), phaseCheck(argo, "status.phase", "Available")}}
	if b.CredentialsRef != "" {
		phase.Secrets = []co.SecretPlacement{{Secret: b.CredentialsRef, Namespace: "openshift-gitops", Name: "bootwright-bootstrap-repository", Type: "Opaque", Keys: []co.KeyPart{{Key: "username", Part: "username"}, {Key: "password", Part: "password"}}, Labels: map[string]string{"argocd.argoproj.io/secret-type": "repository"}, Strings: map[string]string{"type": "git", "url": b.RepoURL}}}
	}
	if b.Access != nil {
		role := co.Ref{APIVersion: "rbac.authorization.k8s.io/v1", Kind: "ClusterRole", Name: b.Access.ClusterRole}
		phase.Gate = append(phase.Gate, co.Check{ResourceExists: &role})
		binding := object("rbac.authorization.k8s.io/v1", "ClusterRoleBinding", "", "bootwright-gitops-bootstrap", api.Value{})
		binding.Body = binding.Body.With("roleRef", api.MapValue(api.FieldValue{Name: "apiGroup", Value: api.StringValue("rbac.authorization.k8s.io")}, api.FieldValue{Name: "kind", Value: api.StringValue("ClusterRole")}, api.FieldValue{Name: "name", Value: api.StringValue(b.Access.ClusterRole)})).With("subjects", api.ListValue(api.MapValue(api.FieldValue{Name: "kind", Value: api.StringValue("ServiceAccount")}, api.FieldValue{Name: "name", Value: api.StringValue("openshift-gitops-argocd-application-controller")}, api.FieldValue{Name: "namespace", Value: api.StringValue("openshift-gitops")})))
		phase.Objects = append(phase.Objects, binding)
	}
	spec := api.MapValue(api.FieldValue{Name: "project", Value: api.StringValue("default")}, api.FieldValue{Name: "source", Value: api.MapValue(api.FieldValue{Name: "repoURL", Value: api.StringValue(b.RepoURL)}, api.FieldValue{Name: "targetRevision", Value: api.StringValue(b.TargetRevision)}, api.FieldValue{Name: "path", Value: api.StringValue(b.Path)})}, api.FieldValue{Name: "destination", Value: api.MapValue(api.FieldValue{Name: "server", Value: api.StringValue("https://kubernetes.default.svc")}, api.FieldValue{Name: "namespace", Value: api.StringValue(b.DestinationNamespace)})})
	if b.Sync.Automated {
		spec = spec.With("syncPolicy", api.MapValue(api.FieldValue{Name: "automated", Value: api.MapValue(api.FieldValue{Name: "prune", Value: api.BoolValue(b.Sync.Prune)}, api.FieldValue{Name: "selfHeal", Value: api.BoolValue(b.Sync.SelfHeal)})}))
	}
	phase.Objects = append(phase.Objects, object("argoproj.io/v1alpha1", "Application", "openshift-gitops", "bootwright-bootstrap", spec))
	result.After = []co.Phase{phase}
	return result, nil
}
