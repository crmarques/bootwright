package desiredstate

import (
	"fmt"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func validateEnvironmentSecretStorage(env v1alpha1.Environment) []string {
	switch env.Spec.SecretStorage.Mode {
	case "", v1alpha1.SecretStorageModeSource, v1alpha1.SecretStorageModeContext:
		return nil
	default:
		return []string{fmt.Sprintf("Environment/%s spec.secretStorage.mode %q must be one of {%s, %s}",
			env.Metadata.Name, env.Spec.SecretStorage.Mode, v1alpha1.SecretStorageModeSource, v1alpha1.SecretStorageModeContext)}
	}
}

// validateSecrets checks each declared Secret in isolation: a known type, at
// most one source arm, and type-scoped file/generated parameters.
func validateSecrets(state v1alpha1.State) []string {
	var errs []string
	for _, s := range state.Secrets {
		if e := validateName(v1alpha1.KindSecret, s.Metadata.Name); e != "" {
			errs = append(errs, e)
			continue
		}
		prefix := fmt.Sprintf("Secret/%s spec", s.Metadata.Name)
		if s.Spec.Type == "" {
			errs = append(errs, prefix+".type is required")
			continue
		}
		if !validSecretType(s.Spec.Type) {
			errs = append(errs, fmt.Sprintf("%s.type %q must be one of {%s}", prefix, s.Spec.Type, strings.Join(v1alpha1.SecretTypes(), ", ")))
			continue
		}
		src := s.Spec.Source
		arms := 0
		for _, set := range []bool{src.ContextStore != nil, src.File != nil, src.Generated != nil} {
			if set {
				arms++
			}
		}
		if arms > 1 {
			errs = append(errs, prefix+".source sets more than one of {contextStore, file, generated}; pick at most one")
			continue
		}
		if src.File != nil {
			errs = append(errs, validateSecretFileSource(prefix, s.Spec.Type, src.File)...)
		}
		if src.Generated != nil {
			errs = append(errs, validateSecretGenerated(prefix, s.Spec.Type, src.Generated)...)
		}
	}
	return errs
}

func validSecretType(secretType string) bool {
	for _, t := range v1alpha1.SecretTypes() {
		if secretType == t {
			return true
		}
	}
	return false
}

// validateSecretFileSource enforces that only the file keys the type consumes
// are set: tlsCertificate uses cert+key, sshKeyPair uses privateKey(+publicKey),
// every other type uses path.
func validateSecretFileSource(prefix, secretType string, f *v1alpha1.SecretFileSource) []string {
	var errs []string
	foreign := func(field, value string) {
		if value != "" {
			errs = append(errs, fmt.Sprintf("%s.source.file.%s is not valid for a %s secret", prefix, field, secretType))
		}
	}
	switch secretType {
	case v1alpha1.SecretTypeTLSCertificate:
		if f.Cert == "" {
			errs = append(errs, prefix+".source.file.cert is required for a tlsCertificate secret")
		}
		if f.Key == "" {
			errs = append(errs, prefix+".source.file.key is required for a tlsCertificate secret")
		}
		foreign("path", f.Path)
		foreign("privateKey", f.PrivateKey)
		foreign("publicKey", f.PublicKey)
	case v1alpha1.SecretTypeSSHKeyPair:
		if f.PrivateKey == "" {
			errs = append(errs, prefix+".source.file.privateKey is required for an sshKeyPair secret")
		}
		foreign("path", f.Path)
		foreign("cert", f.Cert)
		foreign("key", f.Key)
	default:
		if f.Path == "" {
			errs = append(errs, fmt.Sprintf("%s.source.file.path is required for a %s secret", prefix, secretType))
		}
		foreign("cert", f.Cert)
		foreign("key", f.Key)
		foreign("privateKey", f.PrivateKey)
		foreign("publicKey", f.PublicKey)
	}
	return errs
}

// validateSecretGenerated enforces that the type can be generated and that only
// the generation parameters the type consumes are set.
func validateSecretGenerated(prefix, secretType string, gen *v1alpha1.SecretGeneratedSource) []string {
	if !v1alpha1.SecretTypeGeneratable(secretType) {
		return []string{fmt.Sprintf("%s.source.generated is not valid for a %s secret", prefix, secretType)}
	}
	var errs []string
	foreignStr := func(field, value string) {
		if value != "" {
			errs = append(errs, fmt.Sprintf("%s.source.generated.%s is not valid for a %s secret", prefix, field, secretType))
		}
	}
	switch secretType {
	case v1alpha1.SecretTypeUsernamePassword:
		username := gen.Username
		if strings.TrimSpace(username) != username {
			errs = append(errs, prefix+".source.generated.username must not contain leading or trailing whitespace")
		}
		if strings.ContainsAny(username, ":\r\n\t ") {
			errs = append(errs, prefix+".source.generated.username must not contain whitespace, colon, or newlines")
		}
		foreignStr("commonName", gen.CommonName)
		foreignStr("keyType", gen.KeyType)
	case v1alpha1.SecretTypeTLSCertificate, v1alpha1.SecretTypeCABundle:
		if gen.CommonName == "" {
			errs = append(errs, prefix+".source.generated.commonName is required")
		}
		if gen.ValidityDays < 0 {
			errs = append(errs, prefix+".source.generated.validityDays must not be negative")
		}
		foreignStr("username", gen.Username)
		foreignStr("keyType", gen.KeyType)
	case v1alpha1.SecretTypeSSHKeyPair:
		if !validSSHKeyPairType(gen.KeyType) {
			errs = append(errs, fmt.Sprintf("%s.source.generated.keyType %q must be one of {%s}", prefix, gen.KeyType, strings.Join(allowedSSHKeyPairTypes(), ", ")))
		}
		if strings.TrimSpace(gen.Comment) != gen.Comment {
			errs = append(errs, prefix+".source.generated.comment must not contain leading or trailing whitespace")
		}
		if strings.ContainsAny(gen.Comment, "\r\n") {
			errs = append(errs, prefix+".source.generated.comment must not contain newlines")
		}
		foreignStr("username", gen.Username)
		foreignStr("commonName", gen.CommonName)
	case v1alpha1.SecretTypeToken:
		if gen.Bytes < 0 {
			errs = append(errs, prefix+".source.generated.bytes must not be negative")
		}
		foreignStr("username", gen.Username)
		foreignStr("commonName", gen.CommonName)
		foreignStr("keyType", gen.KeyType)
	}
	return errs
}

func validateEnvironmentInstallTrust(env v1alpha1.Environment) []string {
	if env.Spec.InstallTrust == nil {
		return nil
	}
	var errs []string
	seen := map[string]bool{}
	owner := fmt.Sprintf("Environment/%s spec.installTrust.caBundleRefs", env.Metadata.Name)
	for i, ref := range env.Spec.InstallTrust.CABundleRefs {
		if ref.Name == "" {
			errs = append(errs, fmt.Sprintf("%s[%d] is required", owner, i))
			continue
		}
		if seen[ref.Name] {
			errs = append(errs, fmt.Sprintf("%s[%d] %q is duplicated", owner, i, ref.Name))
			continue
		}
		seen[ref.Name] = true
	}
	return errs
}

func validSSHKeyPairType(keyType string) bool {
	if keyType == "" {
		return true
	}
	for _, allowed := range allowedSSHKeyPairTypes() {
		if keyType == allowed {
			return true
		}
	}
	return false
}

func allowedSSHKeyPairTypes() []string {
	return []string{
		v1alpha1.SSHKeyPairTypeEd25519,
		v1alpha1.SSHKeyPairTypeRSA,
		v1alpha1.SSHKeyPairTypeECDSAP256,
		v1alpha1.SSHKeyPairTypeECDSAP384,
		v1alpha1.SSHKeyPairTypeECDSAP521,
	}
}
