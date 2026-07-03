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

func validateEnvironmentSecrets(env v1alpha1.Environment) []string {
	var errs []string
	for name, secret := range env.Spec.Secrets {
		if !dnsLabel.MatchString(name) {
			errs = append(errs, fmt.Sprintf("Environment/%s spec.secrets entry %q is not a DNS label", env.Metadata.Name, name))
			continue
		}
		hasFile := secret.File != ""
		hasGenerated := secret.Generated != nil
		switch {
		case secret.KeyFile != "" && !hasFile:
			errs = append(errs, fmt.Sprintf("Environment/%s spec.secrets[%s].keyFile requires file", env.Metadata.Name, name))
		case hasFile && hasGenerated:
			errs = append(errs, fmt.Sprintf("Environment/%s spec.secrets[%s] sets both file and generated; pick at most one source", env.Metadata.Name, name))
		case hasGenerated:
			errs = append(errs, validateGeneratedSecret(env.Metadata.Name, name, secret.Generated)...)
		}
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

func validateGeneratedSecret(envName, secretName string, gen *v1alpha1.EnvironmentSecretGenerated) []string {
	var errs []string
	kinds := 0
	if gen.Credentials != nil {
		kinds++
	}
	if gen.SelfSignedCertificate != nil {
		kinds++
	}
	if gen.SSHKeyPair != nil {
		kinds++
	}
	switch {
	case kinds > 1:
		errs = append(errs, fmt.Sprintf("Environment/%s spec.secrets[%s].generated sets more than one generated kind; pick exactly one of {credentials, selfSignedCertificate, sshKeyPair}", envName, secretName))
	case kinds == 0:
		errs = append(errs, fmt.Sprintf("Environment/%s spec.secrets[%s].generated requires one of {credentials, selfSignedCertificate, sshKeyPair}", envName, secretName))
	case gen.SelfSignedCertificate != nil:
		if gen.SelfSignedCertificate.CommonName == "" {
			errs = append(errs, fmt.Sprintf("Environment/%s spec.secrets[%s].generated.selfSignedCertificate.commonName is required", envName, secretName))
		}
		if gen.SelfSignedCertificate.ValidityDays < 0 {
			errs = append(errs, fmt.Sprintf("Environment/%s spec.secrets[%s].generated.selfSignedCertificate.validityDays must not be negative", envName, secretName))
		}
	case gen.Credentials != nil:
		username := gen.Credentials.Username
		if username != "" && strings.TrimSpace(username) != username {
			errs = append(errs, fmt.Sprintf("Environment/%s spec.secrets[%s].generated.credentials.username must not contain leading or trailing whitespace", envName, secretName))
		}
		if strings.ContainsAny(username, ":\r\n\t ") {
			errs = append(errs, fmt.Sprintf("Environment/%s spec.secrets[%s].generated.credentials.username must not contain whitespace, colon, or newlines", envName, secretName))
		}
	case gen.SSHKeyPair != nil:
		keyType := gen.SSHKeyPair.Type
		if !validSSHKeyPairType(keyType) {
			errs = append(errs, fmt.Sprintf("Environment/%s spec.secrets[%s].generated.sshKeyPair.type %q must be one of {%s}", envName, secretName, keyType, strings.Join(allowedSSHKeyPairTypes(), ", ")))
		}
		if strings.TrimSpace(gen.SSHKeyPair.Comment) != gen.SSHKeyPair.Comment {
			errs = append(errs, fmt.Sprintf("Environment/%s spec.secrets[%s].generated.sshKeyPair.comment must not contain leading or trailing whitespace", envName, secretName))
		}
		if strings.ContainsAny(gen.SSHKeyPair.Comment, "\r\n") {
			errs = append(errs, fmt.Sprintf("Environment/%s spec.secrets[%s].generated.sshKeyPair.comment must not contain newlines", envName, secretName))
		}
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
