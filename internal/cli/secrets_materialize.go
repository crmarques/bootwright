package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/crmarques/bootwright/api/v1alpha1"
	cliout "github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/safefs"
	"github.com/crmarques/bootwright/internal/secret"
)

// CLI-side orchestration for secret materialization. The pure
// crypto/parsing/validation helpers live in internal/secret; this file
// stays as the dispatch layer that builds the list of work items from
// loaded state and applies them to the secrets directory.

func generatedSelfSignedRequests(state v1alpha1.State) ([]generatedSelfSignedRequest, error) {
	env := primaryEnvironmentForSync(state)
	if env == nil {
		return nil, nil
	}
	names := make([]string, 0, len(env.Spec.Secrets))
	for name, s := range env.Spec.Secrets {
		if s.Generated == nil || s.Generated.SelfSignedCertificate == nil {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	result := make([]generatedSelfSignedRequest, 0, len(names))
	for _, name := range names {
		cert := *env.Spec.Secrets[name].Generated.SelfSignedCertificate
		cert.DNSNames = append([]string(nil), cert.DNSNames...)
		cert.IPAddresses = append([]string(nil), cert.IPAddresses...)
		result = append(result, generatedSelfSignedRequest{name: name, certificate: cert})
	}
	return result, nil
}

type generatedCredentialsRequest struct {
	name        string
	credentials v1alpha1.GeneratedCredentialsSpec
}

func generatedCredentialsRequestsFor(state v1alpha1.State) []generatedCredentialsRequest {
	env := primaryEnvironmentForSync(state)
	if env == nil {
		return nil
	}
	names := make([]string, 0, len(env.Spec.Secrets))
	for name, s := range env.Spec.Secrets {
		if s.Generated == nil || s.Generated.Credentials == nil {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]generatedCredentialsRequest, 0, len(names))
	for _, name := range names {
		out = append(out, generatedCredentialsRequest{name: name, credentials: *env.Spec.Secrets[name].Generated.Credentials})
	}
	return out
}

type generatedSSHKeyPairRequest struct {
	name    string
	keyPair v1alpha1.GeneratedSSHKeyPairSpec
}

func generatedSSHKeyPairRequestsFor(state v1alpha1.State) []generatedSSHKeyPairRequest {
	env := primaryEnvironmentForSync(state)
	if env == nil {
		return nil
	}
	names := make([]string, 0, len(env.Spec.Secrets))
	for name, s := range env.Spec.Secrets {
		if s.Generated == nil || s.Generated.SSHKeyPair == nil {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]generatedSSHKeyPairRequest, 0, len(names))
	for _, name := range names {
		out = append(out, generatedSSHKeyPairRequest{name: name, keyPair: *env.Spec.Secrets[name].Generated.SSHKeyPair})
	}
	return out
}

type secretMaterializeOptions struct {
	Generated   bool
	FileSources bool
}

type secretMaterializeResult struct {
	name   string
	action string
}

func runSecretMaterialize(stdout io.Writer, command, secretsDir string, state v1alpha1.State, opts secretMaterializeOptions, emptyStatus cliout.Status) error {
	results, err := materializeSecrets(secretsDir, state, opts)
	if err != nil {
		return failErr(1, err)
	}
	p := cliout.New(stdout)
	p.Command(command)
	if len(results) == 0 {
		p.Summary(emptyStatus, command, "no secret requests found")
		return nil
	}
	for _, result := range results {
		p.Status(cliout.StatusOK, result.name, result.action)
	}
	p.Summary(cliout.StatusOK, command, fmt.Sprintf("%d secret request(s) handled", len(results)))
	return nil
}

func materializeSecrets(secretsDir string, state v1alpha1.State, opts secretMaterializeOptions) ([]secretMaterializeResult, error) {
	if err := ensureSecretsDir(secretsDir); err != nil {
		return nil, err
	}
	var out []secretMaterializeResult
	if opts.Generated {
		certRequests, err := generatedSelfSignedRequests(state)
		if err != nil {
			return nil, err
		}
		for _, request := range certRequests {
			action, err := materializeSelfSignedCertificate(secretsDir, request)
			if err != nil {
				return nil, err
			}
			out = append(out, secretMaterializeResult{name: request.name, action: action})
		}
		for _, request := range generatedCredentialsRequestsFor(state) {
			action, err := materializeGeneratedCredentials(secretsDir, request)
			if err != nil {
				return nil, err
			}
			out = append(out, secretMaterializeResult{name: request.name, action: action})
		}
		for _, request := range generatedSSHKeyPairRequestsFor(state) {
			action, err := materializeGeneratedSSHKeyPair(secretsDir, request)
			if err != nil {
				return nil, err
			}
			out = append(out, secretMaterializeResult{name: request.name, action: action})
		}
	}
	if opts.FileSources {
		for _, request := range fileSecretCopyRequests(state, secretsDir) {
			action, err := materializeFileSecretCopy(request)
			if err != nil {
				return nil, err
			}
			out = append(out, secretMaterializeResult{name: request.name, action: action})
		}
	}
	return out, nil
}

func ensureSecretsDir(secretsDir string) error {
	if err := os.MkdirAll(secretsDir, 0o700); err != nil {
		return fmt.Errorf("create secrets directory %s: %w", secretsDir, err)
	}
	if err := os.Chmod(secretsDir, 0o700); err != nil {
		return fmt.Errorf("chmod secrets directory %s: %w", secretsDir, err)
	}
	return nil
}

func materializeSelfSignedCertificate(secretsDir string, request generatedSelfSignedRequest) (string, error) {
	certPath := filepath.Join(secretsDir, request.name)
	keyPath := certPath + ".key"
	certExists, err := safefs.RegularFileExists(certPath)
	if err != nil {
		return "", err
	}
	keyExists, err := safefs.RegularFileExists(keyPath)
	if err != nil {
		return "", err
	}
	if certExists != keyExists {
		return "", fmt.Errorf("generated self-signed certificate %q is partially present; expected both %s and %s", request.name, certPath, keyPath)
	}
	if certExists {
		if err := secret.VerifySelfSignedCertificateMatchesRequest(certPath, request.certificate); err != nil {
			return "", fmt.Errorf("existing self-signed certificate %q at %s no longer matches the desired spec: %w; remove %s and %s to regenerate", request.name, certPath, err, certPath, keyPath)
		}
		return "reused existing certificate and key", nil
	}
	certPEM, keyPEM, err := secret.SelfSignedCertificatePEM(request.certificate)
	if err != nil {
		return "", err
	}
	if err := safefs.WriteNewFile(certPath, certPEM, 0o600); err != nil {
		return "", err
	}
	if err := safefs.WriteNewFile(keyPath, keyPEM, 0o600); err != nil {
		_ = os.Remove(certPath)
		return "", err
	}
	return fmt.Sprintf("generated %s and %s", certPath, keyPath), nil
}

func materializeGeneratedCredentials(secretsDir string, request generatedCredentialsRequest) (string, error) {
	target := filepath.Join(secretsDir, request.name)
	wantUser := request.credentials.Username
	if wantUser == "" {
		wantUser = "admin"
	}
	exists, err := safefs.RegularFileExists(target)
	if err != nil {
		return "", err
	}
	if exists {
		data, err := os.ReadFile(target)
		if err != nil {
			return "", fmt.Errorf("read existing credentials %s: %w", target, err)
		}
		gotUser, _, perr := secret.ParseBMCCredentials(data)
		if perr != nil {
			return "", fmt.Errorf("existing credentials %s: %w; remove the file to regenerate", target, perr)
		}
		if gotUser != wantUser {
			return "", fmt.Errorf("existing credentials %q at %s use username %q but desired spec wants %q; remove %s to regenerate", request.name, target, gotUser, wantUser, target)
		}
		return "reused existing credentials", nil
	}
	password, err := secret.GenerateBMCPassword()
	if err != nil {
		return "", err
	}
	payload := []byte(wantUser + ":" + password + "\n")
	if err := safefs.AtomicWriteFile(target, payload, 0o600); err != nil {
		return "", err
	}
	return fmt.Sprintf("generated %s (user %q)", target, wantUser), nil
}

func materializeGeneratedSSHKeyPair(secretsDir string, request generatedSSHKeyPairRequest) (string, error) {
	privatePath := filepath.Join(secretsDir, request.name)
	publicPath := privatePath + ".pub"
	privateExists, err := safefs.RegularFileExists(privatePath)
	if err != nil {
		return "", err
	}
	publicExists, err := safefs.RegularFileExists(publicPath)
	if err != nil {
		return "", err
	}
	if privateExists != publicExists {
		return "", fmt.Errorf("generated SSH key pair %q is partially present; expected both %s and %s", request.name, privatePath, publicPath)
	}
	if privateExists {
		if err := secret.VerifySSHKeyPairPublicMatchesRequest(publicPath, request.keyPair); err != nil {
			return "", fmt.Errorf("existing SSH key pair %q at %s no longer matches the desired spec: %w; remove %s and %s to regenerate", request.name, publicPath, err, privatePath, publicPath)
		}
		return "reused existing SSH key pair", nil
	}
	privateKey, publicKey, err := secret.SSHKeyPairPEM(request.keyPair)
	if err != nil {
		return "", err
	}
	if err := safefs.WriteNewFile(privatePath, privateKey, 0o600); err != nil {
		return "", err
	}
	if err := safefs.WriteNewFile(publicPath, publicKey, 0o600); err != nil {
		_ = os.Remove(privatePath)
		return "", err
	}
	return fmt.Sprintf("generated %s and %s", privatePath, publicPath), nil
}

type fileSecretCopyRequest struct {
	name   string
	source string
	target string
}

func fileSecretCopyRequests(state v1alpha1.State, secretsDir string) []fileSecretCopyRequest {
	env := primaryEnvironmentForSync(state)
	if env == nil || env.Spec.SecretStorage.Mode != v1alpha1.SecretStorageModeContext {
		return nil
	}
	names := make([]string, 0, len(env.Spec.Secrets))
	for name, spec := range env.Spec.Secrets {
		if spec.File != "" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	var out []fileSecretCopyRequest
	seen := map[string]bool{}
	add := func(name string, role secret.MaterialRole) {
		sourcePath := secret.ResolveSourceMaterialPath(name, env, role)
		targetPath := secret.ResolveMaterialPath(name, env, secretsDir, role)
		if sourcePath == "" || targetPath == "" {
			return
		}
		key := name + "\x00" + sourcePath + "\x00" + targetPath
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, fileSecretCopyRequest{name: name, source: sourcePath, target: targetPath})
	}
	for _, name := range names {
		added := false
		if secretConsumedAsTLS(name, state) {
			add(name, secret.MaterialPrimary)
			add(name, secret.MaterialTLSKey)
			added = true
		}
		if secretConsumedAsClusterSSHPublic(name, state) {
			add(name, secret.MaterialSSHPublic)
			added = true
		}
		if secretConsumedAsClusterSSHPrivate(name, state) || secretConsumedAsHostSSH(name, state) {
			add(name, secret.MaterialSSHPrivate)
			added = true
		}
		if !added {
			add(name, secret.MaterialPrimary)
		}
		if env.Spec.Secrets[name].KeyFile != "" {
			add(name, secret.MaterialTLSKey)
		}
	}
	return out
}

func materializeFileSecretCopy(request fileSecretCopyRequest) (string, error) {
	if filepath.Clean(request.source) == filepath.Clean(request.target) {
		return "source already context-local at " + request.target, nil
	}
	data, err := secret.ReadFile(request.source)
	if err != nil {
		return "", fmt.Errorf("read file-sourced secret %s for %s: %w", request.source, request.name, err)
	}
	exists, err := safefs.RegularFileExists(request.target)
	if err != nil {
		return "", err
	}
	if err := safefs.AtomicWriteFile(request.target, data, 0o600); err != nil {
		return "", err
	}
	action := "copied"
	if exists {
		action = "updated"
	}
	return fmt.Sprintf("%s %s to %s", action, request.source, request.target), nil
}
