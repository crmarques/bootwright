package secret

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"path/filepath"
	"sort"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

type MaterializeOptions struct {
	Generated   bool
	FileSources bool
	Renew       bool
}

type MaterializeResult struct {
	Name   string
	Action string
}

type GeneratedSelfSignedRequest struct {
	Name        string
	Certificate v1alpha1.SelfSignedCertificateSpec
}

func GeneratedSelfSignedRequests(state v1alpha1.State) ([]GeneratedSelfSignedRequest, error) {
	secrets := generatedSecrets(state, v1alpha1.SecretTypeSelfSigned)
	result := make([]GeneratedSelfSignedRequest, 0, len(secrets))
	for _, s := range secrets {
		cert := s.Spec.Source.Generated.SelfSignedCertificate()
		cert.DNSNames = append([]string(nil), cert.DNSNames...)
		cert.IPAddresses = append([]string(nil), cert.IPAddresses...)
		result = append(result, GeneratedSelfSignedRequest{Name: s.Metadata.Name, Certificate: cert})
	}
	return result, nil
}

func generatedSecretsOfType(state v1alpha1.State, secretType string) []v1alpha1.Secret {
	return generatedSecrets(state, func(t string) bool { return t == secretType })
}

func generatedSecrets(state v1alpha1.State, match func(string) bool) []v1alpha1.Secret {
	var out []v1alpha1.Secret
	for _, s := range state.Secrets {
		if s.Spec.Source.Generated != nil && match(s.Spec.Type) {
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Metadata.Name < out[j].Metadata.Name })
	return out
}

type generatedCredentialsRequest struct {
	name        string
	credentials v1alpha1.GeneratedCredentialsSpec
}

func generatedCredentialsRequestsFor(state v1alpha1.State) []generatedCredentialsRequest {
	secrets := generatedSecretsOfType(state, v1alpha1.SecretTypeUsernamePassword)
	out := make([]generatedCredentialsRequest, 0, len(secrets))
	for _, s := range secrets {
		out = append(out, generatedCredentialsRequest{name: s.Metadata.Name, credentials: s.Spec.Source.Generated.GeneratedCredentials()})
	}
	return out
}

type generatedTokenRequest struct {
	name  string
	bytes int
}

func generatedTokenRequestsFor(state v1alpha1.State) []generatedTokenRequest {
	secrets := generatedSecretsOfType(state, v1alpha1.SecretTypeToken)
	out := make([]generatedTokenRequest, 0, len(secrets))
	for _, s := range secrets {
		size := s.Spec.Source.Generated.Bytes
		if size <= 0 {
			size = v1alpha1.DefaultTokenBytes
		}
		out = append(out, generatedTokenRequest{name: s.Metadata.Name, bytes: size})
	}
	return out
}

type generatedSSHKeyPairRequest struct {
	name    string
	keyPair v1alpha1.GeneratedSSHKeyPairSpec
}

func generatedSSHKeyPairRequestsFor(state v1alpha1.State) []generatedSSHKeyPairRequest {
	secrets := generatedSecretsOfType(state, v1alpha1.SecretTypeSSHKeyPair)
	out := make([]generatedSSHKeyPairRequest, 0, len(secrets))
	for _, s := range secrets {
		out = append(out, generatedSSHKeyPairRequest{name: s.Metadata.Name, keyPair: s.Spec.Source.Generated.GeneratedSSHKeyPair()})
	}
	return out
}

func MaterializeForContext(contextName, secretsDir string, state v1alpha1.State, opts MaterializeOptions) ([]MaterializeResult, error) {
	store := NewContextStore(contextName, secretsDir)
	var out []MaterializeResult
	if opts.Generated {
		certRequests, err := GeneratedSelfSignedRequests(state)
		if err != nil {
			return nil, err
		}
		for _, request := range certRequests {
			action, err := materializeSelfSignedCertificate(store, secretsDir, request, opts.Renew)
			if err != nil {
				return nil, err
			}
			out = append(out, MaterializeResult{Name: request.Name, Action: action})
		}
		for _, request := range generatedCredentialsRequestsFor(state) {
			action, err := materializeGeneratedCredentials(store, secretsDir, request, opts.Renew)
			if err != nil {
				return nil, err
			}
			out = append(out, MaterializeResult{Name: request.name, Action: action})
		}
		for _, request := range generatedSSHKeyPairRequestsFor(state) {
			action, err := materializeGeneratedSSHKeyPair(store, secretsDir, request, opts.Renew)
			if err != nil {
				return nil, err
			}
			out = append(out, MaterializeResult{Name: request.name, Action: action})
		}
		for _, request := range generatedTokenRequestsFor(state) {
			action, err := materializeGeneratedToken(store, secretsDir, request, opts.Renew)
			if err != nil {
				return nil, err
			}
			out = append(out, MaterializeResult{Name: request.name, Action: action})
		}
	}
	if opts.FileSources {
		for _, request := range fileSecretCopyRequests(state, secretsDir) {
			action, err := materializeFileSecretCopy(store, request)
			if err != nil {
				return nil, err
			}
			out = append(out, MaterializeResult{Name: request.name, Action: action})
		}
	}
	return out, nil
}

func materializeSelfSignedCertificate(store *ContextStore, secretsDir string, request GeneratedSelfSignedRequest, renew bool) (string, error) {
	certPath := filepath.Join(secretsDir, request.Name)
	keyPath := certPath + ".key"
	certKey := MaterialKey{Name: request.Name, Role: MaterialPrimary}
	keyKey := MaterialKey{Name: request.Name, Role: MaterialTLSKey}
	certExists, err := store.Exists(certKey)
	if err != nil {
		return "", err
	}
	keyExists, err := store.Exists(keyKey)
	if err != nil {
		return "", err
	}
	if !renew {
		if certExists != keyExists {
			return "", fmt.Errorf("generated self-signed certificate %q is partially present; expected both %s and %s", request.Name, certPath, keyPath)
		}
		if certExists {
			data, err := store.Read(certKey)
			if err != nil {
				return "", err
			}
			if err := VerifySelfSignedCertificateBytesMatchRequest(data, request.Certificate); err != nil {
				return "", fmt.Errorf("existing self-signed certificate %q at %s no longer matches the desired spec: %w; run bootwright secret generate --renew to regenerate", request.Name, certPath, err)
			}
			return "reused existing certificate and key", nil
		}
	}
	certPEM, keyPEM, err := SelfSignedCertificatePEM(request.Certificate)
	if err != nil {
		return "", err
	}
	if err := store.Write(certKey, certPEM); err != nil {
		return "", err
	}
	if err := store.Write(keyKey, keyPEM); err != nil {
		_ = store.Delete(certKey)
		return "", err
	}
	if renew && (certExists || keyExists) {
		return fmt.Sprintf("regenerated %s and %s", certPath, keyPath), nil
	}
	return fmt.Sprintf("generated %s and %s", certPath, keyPath), nil
}

func materializeGeneratedCredentials(store *ContextStore, secretsDir string, request generatedCredentialsRequest, renew bool) (string, error) {
	target := filepath.Join(secretsDir, request.name)
	wantUser := request.credentials.Username
	if wantUser == "" {
		wantUser = "admin"
	}
	key := MaterialKey{Name: request.name, Role: MaterialPrimary}
	exists, err := store.Exists(key)
	if err != nil {
		return "", err
	}
	if exists && !renew {
		data, err := store.Read(key)
		if err != nil {
			return "", fmt.Errorf("read existing credentials %s: %w", target, err)
		}
		gotUser, _, perr := ParseBMCCredentials(data)
		if perr != nil {
			return "", fmt.Errorf("existing credentials %s: %w; run bootwright secret generate --renew to regenerate", target, perr)
		}
		if gotUser != wantUser {
			return "", fmt.Errorf("existing credentials %q at %s use username %q but desired spec wants %q; run bootwright secret generate --renew to regenerate", request.name, target, gotUser, wantUser)
		}
		return "reused existing credentials", nil
	}
	password, err := GenerateBMCPassword()
	if err != nil {
		return "", err
	}
	payload := []byte(wantUser + ":" + password + "\n")
	if err := store.Write(key, payload); err != nil {
		return "", err
	}
	if renew && exists {
		return fmt.Sprintf("regenerated %s (user %q)", target, wantUser), nil
	}
	return fmt.Sprintf("generated %s (user %q)", target, wantUser), nil
}

func materializeGeneratedToken(store *ContextStore, secretsDir string, request generatedTokenRequest, renew bool) (string, error) {
	target := filepath.Join(secretsDir, request.name)
	key := MaterialKey{Name: request.name, Role: MaterialPrimary}
	exists, err := store.Exists(key)
	if err != nil {
		return "", err
	}
	if exists && !renew {
		return "reused existing token", nil
	}
	buf := make([]byte, request.bytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate token %s: %w", request.name, err)
	}
	if err := store.Write(key, []byte(base64.RawURLEncoding.EncodeToString(buf)+"\n")); err != nil {
		return "", err
	}
	if renew && exists {
		return "regenerated " + target, nil
	}
	return "generated " + target, nil
}

func materializeGeneratedSSHKeyPair(store *ContextStore, secretsDir string, request generatedSSHKeyPairRequest, renew bool) (string, error) {
	privatePath := filepath.Join(secretsDir, request.name)
	publicPath := privatePath + ".pub"
	privateMaterial := MaterialKey{Name: request.name, Role: MaterialSSHPrivate}
	publicMaterial := MaterialKey{Name: request.name, Role: MaterialSSHPublic}
	privateExists, err := store.Exists(privateMaterial)
	if err != nil {
		return "", err
	}
	publicExists, err := store.Exists(publicMaterial)
	if err != nil {
		return "", err
	}
	if !renew {
		if privateExists != publicExists {
			return "", fmt.Errorf("generated SSH key pair %q is partially present; expected both %s and %s", request.name, privatePath, publicPath)
		}
		if privateExists {
			data, err := store.Read(publicMaterial)
			if err != nil {
				return "", err
			}
			if err := VerifySSHKeyPairPublicBytesMatchRequest(data, request.keyPair); err != nil {
				return "", fmt.Errorf("existing SSH key pair %q at %s no longer matches the desired spec: %w; run bootwright secret generate --renew to regenerate", request.name, publicPath, err)
			}
			return "reused existing SSH key pair", nil
		}
	}
	privateKeyPEM, publicKey, err := SSHKeyPairPEM(request.keyPair)
	if err != nil {
		return "", err
	}
	if err := store.Write(privateMaterial, privateKeyPEM); err != nil {
		return "", err
	}
	if err := store.Write(publicMaterial, publicKey); err != nil {
		_ = store.Delete(privateMaterial)
		return "", err
	}
	if renew && (privateExists || publicExists) {
		return fmt.Sprintf("regenerated %s and %s", privatePath, publicPath), nil
	}
	return fmt.Sprintf("generated %s and %s", privatePath, publicPath), nil
}

type fileSecretCopyRequest struct {
	name   string
	role   MaterialRole
	source string
	target string
}

func fileSecretCopyRequests(state v1alpha1.State, secretsDir string) []fileSecretCopyRequest {
	if len(state.Environments) == 0 || state.Environments[0].Spec.SecretStorage.Mode != v1alpha1.SecretStorageModeContext {
		return nil
	}
	fileSecrets := make([]v1alpha1.Secret, 0, len(state.Secrets))
	for _, s := range state.Secrets {
		if s.Spec.Source.File != nil {
			fileSecrets = append(fileSecrets, s)
		}
	}
	sort.Slice(fileSecrets, func(i, j int) bool { return fileSecrets[i].Metadata.Name < fileSecrets[j].Metadata.Name })
	idx := NewIndex(state)
	var out []fileSecretCopyRequest
	seen := map[string]bool{}
	add := func(name string, role MaterialRole) {
		sourcePath := ResolveSourceMaterialPath(name, idx, role)
		targetPath := ResolveMaterialPath(name, idx, secretsDir, role)
		if sourcePath == "" || targetPath == "" {
			return
		}
		key := name + "\x00" + sourcePath + "\x00" + targetPath
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, fileSecretCopyRequest{name: name, role: role, source: sourcePath, target: targetPath})
	}
	for _, s := range fileSecrets {
		name := s.Metadata.Name
		added := false
		if ConsumedAsTLS(name, state) {
			add(name, MaterialPrimary)
			add(name, MaterialTLSKey)
			added = true
		}
		if ConsumedAsClusterSSHPublic(name, state) || ConsumedAsStorageSSHPublic(name, state) {
			add(name, MaterialSSHPublic)
			added = true
		}
		if ConsumedAsClusterSSHPrivate(name, state) || ConsumedAsStorageSSHPrivate(name, state) || ConsumedAsHostSSH(name, state) {
			add(name, MaterialSSHPrivate)
			added = true
		}
		if !added {
			add(name, MaterialPrimary)
		}
		if s.Spec.Type == v1alpha1.SecretTypeTLSCertificate && s.Spec.Source.File.Key != "" {
			add(name, MaterialTLSKey)
		}
	}
	return out
}

func materializeFileSecretCopy(store *ContextStore, request fileSecretCopyRequest) (string, error) {
	if filepath.Clean(request.source) == filepath.Clean(request.target) {
		return "source already context-local at " + request.target, nil
	}
	data, err := ReadExternalFile(request.source)
	if err != nil {
		return "", fmt.Errorf("read file-sourced secret %s for %s: %w", request.source, request.name, err)
	}
	key := MaterialKey{Name: request.name, Role: request.role}
	exists, err := store.Exists(key)
	if err != nil {
		return "", err
	}
	if err := store.Write(key, data); err != nil {
		return "", err
	}
	action := "copied"
	if exists {
		action = "updated"
	}
	return fmt.Sprintf("%s %s to %s", action, request.source, request.target), nil
}
