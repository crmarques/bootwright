package converge

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/host/gitcontent"
	secret "github.com/crmarques/bootwright/internal/secrets"
)

type gitSourceRequest struct {
	key    string
	owner  string
	source v1alpha1.PlaybookGitSource
}

func gitSourceRequests(state v1alpha1.State) []gitSourceRequest {
	var out []gitSourceRequest
	for _, p := range state.Playbooks {
		if !v1alpha1.PlaybookIsEnabled(p) || !v1alpha1.PlaybookSourceIsGit(p.Spec.Source) {
			continue
		}
		out = append(out, gitSourceRequest{
			key:    workflow.GitSourceRootKeyForPlaybook(p.Metadata.Name),
			owner:  "Playbook/" + p.Metadata.Name,
			source: *p.Spec.Source.Git,
		})
	}
	for _, addon := range state.ClusterAddons {
		for _, step := range addon.Spec.Steps {
			if !v1alpha1.PlaybookSourceIsGit(step.Source) {
				continue
			}
			out = append(out, gitSourceRequest{
				key:    workflow.GitSourceRootKeyForStep(addon.Metadata.Name, step.Name),
				owner:  fmt.Sprintf("ClusterAddon/%s step %s", addon.Metadata.Name, step.Name),
				source: *step.Source.Git,
			})
		}
	}
	return out
}

func StateHasGitSources(state v1alpha1.State) bool {
	return len(gitSourceRequests(state)) > 0
}

func ResolveGitSources(ctx context.Context, state v1alpha1.State, contextName, secretsDir, cacheDir string) (map[string]string, error) {
	requests := gitSourceRequests(state)
	if len(requests) == 0 {
		return nil, nil
	}
	gitDir := filepath.Join(cacheDir, "git")
	if err := os.MkdirAll(gitDir, 0o700); err != nil {
		return nil, err
	}
	store := secret.NewContextStore(contextName, secretsDir)
	declared := map[string]v1alpha1.Secret{}
	for _, s := range state.Secrets {
		declared[s.Metadata.Name] = s
	}
	roots := map[string]string{}
	for _, req := range requests {
		credential, err := gitCredential(store, declared, req.source, gitDir)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", req.owner, err)
		}
		result, err := gitcontent.Fetch(ctx, gitcontent.Request{
			URL:        req.source.URL,
			Ref:        req.source.Ref,
			Subdir:     req.source.Subdir,
			Credential: credential,
		}, gitDir)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", req.owner, err)
		}
		roots[req.key] = result.Root
	}
	return roots, nil
}

func gitCredential(store *secret.ContextStore, declared map[string]v1alpha1.Secret, source v1alpha1.PlaybookGitSource, scratchDir string) (*gitcontent.Credential, error) {
	if source.SecretRef == nil {
		return nil, nil
	}
	name := source.SecretRef.Name
	object, ok := declared[name]
	if !ok {
		return nil, fmt.Errorf("secret %q is not declared", name)
	}
	if object.Spec.Type == v1alpha1.SecretTypeSSHKeyPair {
		private, err := store.Read(secret.MaterialKey{Name: name, Role: secret.MaterialSSHPrivate})
		if err != nil {
			return nil, fmt.Errorf("read ssh private key from secret %q: %w", name, err)
		}
		keyDir, err := os.MkdirTemp(scratchDir, "git-key-")
		if err != nil {
			return nil, err
		}
		keyPath := filepath.Join(keyDir, "id")
		if err := os.WriteFile(keyPath, private, 0o600); err != nil {
			return nil, err
		}
		return &gitcontent.Credential{PrivateKeyPath: keyPath}, nil
	}
	password, err := store.Read(secret.MaterialKey{Name: name, Role: secret.MaterialPrimary})
	if err != nil {
		return nil, fmt.Errorf("read secret %q for git authentication: %w", name, err)
	}
	username := "bootwright"
	if object.Spec.Source.Generated != nil && object.Spec.Source.Generated.Username != "" {
		username = object.Spec.Source.Generated.Username
	}
	return &gitcontent.Credential{Username: username, Password: string(password)}, nil
}

func GitSourceCacheDir(runsDir string) string {
	return filepath.Join(runsDir, "content")
}
