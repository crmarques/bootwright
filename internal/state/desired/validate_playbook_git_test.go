package desiredstate

import (
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func gitState(p v1alpha1.Playbook, secrets ...v1alpha1.Secret) v1alpha1.State {
	state := provisioningState(p)
	state.Secrets = secrets
	return state
}

func gitPlaybook(git v1alpha1.PlaybookGitSource) v1alpha1.Playbook {
	p := basePlaybook("external")
	p.Spec.Source = &v1alpha1.PlaybookSource{Git: &git}
	p.Spec.Playbook = "playbooks/site.yml"
	return p
}

func TestValidatePlaybookGitSourceAcceptsPinnedRemote(t *testing.T) {
	p := gitPlaybook(v1alpha1.PlaybookGitSource{
		URL:    "ssh://git@git.example/infra/os-hardening.git",
		Ref:    "v1.4.0",
		Subdir: "bootwright",
	})
	if errs := validatePlaybooks(gitState(p)); len(errs) != 0 {
		t.Fatalf("pinned git source reported errors: %v", errs)
	}
}

func TestValidatePlaybookGitSourceAcceptsBranchRef(t *testing.T) {
	p := gitPlaybook(v1alpha1.PlaybookGitSource{URL: "https://git.example/infra.git", Ref: "main"})
	if errs := validatePlaybooks(gitState(p)); len(errs) != 0 {
		t.Fatalf("branch ref should be allowed: %v", errs)
	}
}

func TestValidatePlaybookGitSourceRejectsBadShape(t *testing.T) {
	cases := []struct {
		name string
		git  v1alpha1.PlaybookGitSource
		want string
	}{
		{"http", v1alpha1.PlaybookGitSource{URL: "http://git.example/i.git", Ref: "main"}, "uses http"},
		{"transport-helper", v1alpha1.PlaybookGitSource{URL: "ext::sh -c id", Ref: "main"}, "transport helper"},
		{"no-ref", v1alpha1.PlaybookGitSource{URL: "https://git.example/i.git"}, "ref is required"},
		{"escaping-subdir", v1alpha1.PlaybookGitSource{URL: "https://git.example/i.git", Ref: "main", Subdir: "../x"}, "must stay within the repository"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if errs := validatePlaybooks(gitState(gitPlaybook(tc.git))); !containsSubstring(errs, tc.want) {
				t.Fatalf("findings %v do not contain %q", errs, tc.want)
			}
		})
	}
}

func TestValidatePlaybookGitSourceRejectsBothArms(t *testing.T) {
	p := gitPlaybook(v1alpha1.PlaybookGitSource{URL: "https://git.example/i.git", Ref: "main"})
	p.Spec.Source.Path = "/srv/ansible"
	if errs := validatePlaybooks(gitState(p)); !containsSubstring(errs, "exactly one of path or git") {
		t.Fatalf("both arms not reported: %v", errs)
	}
}

func TestValidatePlaybookGitSecretRefMustMatchTransport(t *testing.T) {
	sshSecret := v1alpha1.Secret{
		Metadata: v1alpha1.Metadata{Name: "git-key"},
		Spec:     v1alpha1.SecretSpec{Type: v1alpha1.SecretTypeSSHKeyPair},
	}
	tokenSecret := v1alpha1.Secret{
		Metadata: v1alpha1.Metadata{Name: "git-token"},
		Spec:     v1alpha1.SecretSpec{Type: v1alpha1.SecretTypeToken},
	}

	ok := gitPlaybook(v1alpha1.PlaybookGitSource{
		URL: "ssh://git@git.example/i.git", Ref: "main",
		SecretRef: &v1alpha1.SecretRef{Name: "git-key"},
	})
	if errs := validatePlaybooks(gitState(ok, sshSecret)); len(errs) != 0 {
		t.Fatalf("ssh url with sshKeyPair secret reported errors: %v", errs)
	}

	mismatched := gitPlaybook(v1alpha1.PlaybookGitSource{
		URL: "ssh://git@git.example/i.git", Ref: "main",
		SecretRef: &v1alpha1.SecretRef{Name: "git-token"},
	})
	if errs := validatePlaybooks(gitState(mismatched, tokenSecret)); !containsSubstring(errs, "authenticates https") {
		t.Fatalf("transport mismatch not reported: %v", errs)
	}

	undeclared := gitPlaybook(v1alpha1.PlaybookGitSource{
		URL: "https://git.example/i.git", Ref: "main",
		SecretRef: &v1alpha1.SecretRef{Name: "absent"},
	})
	if errs := validatePlaybooks(gitState(undeclared)); !containsSubstring(errs, "does not match any Secret") {
		t.Fatalf("undeclared secret not reported: %v", errs)
	}
}
