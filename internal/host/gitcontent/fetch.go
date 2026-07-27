package gitcontent

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type Credential struct {
	Username       string
	Password       string
	PrivateKeyPath string
}

type Request struct {
	URL        string
	Ref        string
	Subdir     string
	Credential *Credential
}

type Result struct {
	Root   string
	Commit string
}

var hardenedArgs = []string{
	"-c", "core.hooksPath=/dev/null",
	"-c", "protocol.ext.allow=never",
	"-c", "advice.detachedHead=false",
}

func ResolveCommit(ctx context.Context, req Request, workDir string) (string, error) {
	ref := strings.TrimSpace(req.Ref)
	if CommitRe.MatchString(ref) {
		return ref, nil
	}
	out, err := run(ctx, req, workDir, "", append(append([]string(nil), hardenedArgs...), "ls-remote", "--", repoTarget(req.URL), ref)...)
	if err != nil {
		return "", fmt.Errorf("resolve ref %q in %s: %w", ref, req.URL, err)
	}
	var fallback string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		switch fields[1] {
		case "refs/tags/" + ref + "^{}":
			return fields[0], nil
		case "refs/tags/" + ref, "refs/heads/" + ref, ref:
			if fallback == "" {
				fallback = fields[0]
			}
		}
	}
	if fallback != "" {
		return fallback, nil
	}
	return "", fmt.Errorf("ref %q does not resolve to a commit in %s", ref, req.URL)
}

func Fetch(ctx context.Context, req Request, cacheDir string) (Result, error) {
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		return Result{}, err
	}
	commit, err := ResolveCommit(ctx, req, cacheDir)
	if err != nil {
		return Result{}, err
	}
	dest := filepath.Join(cacheDir, commit)
	root := dest
	if sub := strings.TrimSpace(req.Subdir); sub != "" {
		root = filepath.Join(dest, filepath.Clean(sub))
	}
	if _, statErr := os.Stat(dest); statErr == nil {
		return Result{Root: root, Commit: commit}, requireDir(root, req.Subdir)
	}

	staging := dest + ".partial"
	if err := os.RemoveAll(staging); err != nil {
		return Result{}, err
	}
	if err := os.MkdirAll(staging, 0o700); err != nil {
		return Result{}, err
	}
	defer os.RemoveAll(staging)

	if _, err := run(ctx, req, staging, staging, append(append([]string(nil), hardenedArgs...), "init", "--quiet")...); err != nil {
		return Result{}, fmt.Errorf("initialise clone of %s: %w", req.URL, err)
	}
	fetchArgs := append(append([]string(nil), hardenedArgs...),
		"fetch", "--quiet", "--depth", "1", "--no-tags", "--no-recurse-submodules", "--", repoTarget(req.URL), commit)
	if _, err := run(ctx, req, staging, staging, fetchArgs...); err != nil {
		return Result{}, fmt.Errorf("fetch %s from %s: %w", commit, req.URL, err)
	}
	if _, err := run(ctx, req, staging, staging, append(append([]string(nil), hardenedArgs...), "checkout", "--quiet", "FETCH_HEAD")...); err != nil {
		return Result{}, fmt.Errorf("check out %s from %s: %w", commit, req.URL, err)
	}
	if err := os.RemoveAll(filepath.Join(staging, ".git")); err != nil {
		return Result{}, err
	}
	if err := os.Rename(staging, dest); err != nil {
		return Result{}, err
	}
	return Result{Root: root, Commit: commit}, requireDir(root, req.Subdir)
}

func repoTarget(raw string) string {
	if IsLocal(raw) {
		return LocalPath(raw)
	}
	return strings.TrimSpace(raw)
}

func requireDir(root, subdir string) error {
	info, err := os.Stat(root)
	if err != nil {
		if strings.TrimSpace(subdir) != "" {
			return fmt.Errorf("subdir %q does not exist in the fetched repository", subdir)
		}
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("resolved source root %s is not a directory", root)
	}
	return nil
}

func run(ctx context.Context, req Request, scratchDir, workDir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = workDir
	env := append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	cleanup, credEnv, err := credentialEnv(req, scratchDir)
	if err != nil {
		return "", err
	}
	defer cleanup()
	env = append(env, credEnv...)
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		detail := strings.TrimSpace(string(out))
		if detail == "" {
			return "", err
		}
		return "", fmt.Errorf("%w: %s", err, redact(detail, req))
	}
	return string(out), nil
}

func redact(detail string, req Request) string {
	if req.Credential == nil {
		return detail
	}
	if pw := req.Credential.Password; pw != "" {
		detail = strings.ReplaceAll(detail, pw, "[redacted]")
	}
	return detail
}
