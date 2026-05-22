package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/crmarques/bootwright/api/v1alpha1"
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
