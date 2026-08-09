package converge

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	secret "github.com/crmarques/bootwright/internal/secrets"
)

func seedGitSourceRepository(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	run("init", "--quiet", "--initial-branch", "main")
	if err := os.WriteFile(filepath.Join(dir, "site.yml"), []byte("---\n- hosts: all\n"), 0o600); err != nil {
		t.Fatalf("write repository content: %v", err)
	}
	run("add", "-A")
	run("commit", "--quiet", "-m", "seed")
	return dir
}

func gitSourceTestState(repository, ref string) v1alpha1.State {
	return v1alpha1.State{
		Secrets: []v1alpha1.Secret{{
			Metadata: v1alpha1.Metadata{Name: "git-key"},
			Spec:     v1alpha1.SecretSpec{Type: v1alpha1.SecretTypeSSHKeyPair},
		}},
		CustomPlaybooks: []v1alpha1.CustomPlaybook{{
			Metadata: v1alpha1.Metadata{Name: "external"},
			Spec: v1alpha1.CustomPlaybookSpec{
				Source: &v1alpha1.PlaybookSource{Git: &v1alpha1.PlaybookGitSource{
					URL: repository, Ref: ref, SecretRef: &v1alpha1.SecretRef{Name: "git-key"},
				}},
				Playbook: "site.yml",
			},
		}},
	}
}

func writeGitSourcePrivateKey(t *testing.T, secretsDir string) {
	t.Helper()
	store := secret.NewContextStore("test", secretsDir)
	if err := store.Write(secret.MaterialKey{Name: "git-key", Role: secret.MaterialSSHPrivate}, []byte("temporary private key\n")); err != nil {
		t.Fatalf("write git source private key: %v", err)
	}
}

func assertNoGitCredentialResidue(t *testing.T, cacheDir string) {
	t.Helper()
	for _, pattern := range []string{"git-key-*", "git-cred-*"} {
		matches, err := filepath.Glob(filepath.Join(cacheDir, "git", pattern))
		if err != nil {
			t.Fatalf("glob credential residue: %v", err)
		}
		if len(matches) != 0 {
			t.Fatalf("temporary decrypted git credential survived: %v", matches)
		}
	}
}

func TestResolveGitSourcesRemovesSSHCredentialOnSuccessAndFailure(t *testing.T) {
	repository := seedGitSourceRepository(t)
	for _, tc := range []struct {
		name    string
		ref     string
		wantErr bool
	}{
		{name: "success", ref: "main"},
		{name: "failure", ref: "missing", wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			secretsDir := t.TempDir()
			cacheDir := t.TempDir()
			writeGitSourcePrivateKey(t, secretsDir)
			_, err := ResolveGitSources(context.Background(), gitSourceTestState(repository, tc.ref), "test", secretsDir, cacheDir)
			if (err != nil) != tc.wantErr {
				t.Fatalf("ResolveGitSources error = %v, wantErr %v", err, tc.wantErr)
			}
			assertNoGitCredentialResidue(t, cacheDir)
		})
	}
}
