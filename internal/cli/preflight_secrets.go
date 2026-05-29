package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/infra/locality"
	"github.com/crmarques/bootwright/internal/runtime/fs"
	"github.com/crmarques/bootwright/internal/runtime/root/callerio"
	"github.com/crmarques/bootwright/internal/runtime/secrets"
)

type secretRefRequirement struct {
	refName       string
	label         string
	phases        []string
	source        secretRefSource
	generatedKind string
	role          secret.MaterialRole
	tlsPair       bool
	sshPair       bool
}

type secretRefSource string

const (
	secretRefSourceFile      secretRefSource = "file"
	secretRefSourceContext   secretRefSource = "context"
	secretRefSourceGenerated secretRefSource = "generated"
)

func secretRefChecks(state v1alpha1.State, secretsDir string, selected []Phase, deps preflightDeps) []preflightCheck {
	return secretRefChecksWithLocalityPolicy(state, secretsDir, selected, deps, locality.DefaultPolicy)
}

func secretRefChecksWithLocalityPolicy(state v1alpha1.State, secretsDir string, selected []Phase, deps preflightDeps, localPolicy locality.Policy) []preflightCheck {
	requirements := collectSecretRefRequirementsWithLocalityPolicy(state, localPolicy)
	var inScope []secretRefRequirement
	needsSecretsDir := false
	for _, req := range requirements {
		if !anyPhaseInScope(req.phases, selected) {
			continue
		}
		if req.source == secretRefSourceGenerated || req.source == secretRefSourceContext {
			needsSecretsDir = true
		}
		inScope = append(inScope, req)
	}
	if len(inScope) == 0 {
		return nil
	}
	env := environmentForChecks(state)
	var checks []preflightCheck
	if needsSecretsDir {
		checks = append(checks, secretsDirCheck(secretsDir, deps))
	}
	for _, req := range inScope {
		if req.tlsPair {
			checks = append(checks, tlsSecretFileChecks(req, env, secretsDir, deps)...)
			continue
		}
		if req.sshPair {
			checks = append(checks, sshKeyPairFileChecks(req, env, secretsDir, deps)...)
			continue
		}
		if req.source == secretRefSourceGenerated && req.generatedKind == "sshKeyPair" {
			checks = append(checks, generatedSSHKeyPairChecks(req, env, secretsDir, deps)...)
			continue
		}
		if req.source == secretRefSourceGenerated {
			path := filepath.Join(secretsDir, req.refName)
			checks = append(checks, generatedSecretCheck(req.refName, path, req.label, req.generatedKind, deps))
			continue
		}
		path := secret.ResolveMaterialPath(req.refName, env, secretsDir, req.role)
		checks = append(checks, secretFileCheck(req.refName, path, req.label, req.role == secret.MaterialSSHPublic, req.source == secretRefSourceContext, deps))
	}
	return checks
}

func collectSecretRefRequirementsWithLocalityPolicy(state v1alpha1.State, localPolicy locality.Policy) []secretRefRequirement {
	var out []secretRefRequirement

	if env := environmentForChecks(state); env != nil {
		if env.Spec.InstallTrust != nil {
			for i, ref := range env.Spec.InstallTrust.CABundleRefs {
				if ref.Name == "" {
					continue
				}
				out = append(out, secretRefRequirement{
					refName: ref.Name,
					label:   fmt.Sprintf("environment installTrust caBundleRefs[%d]", i),
					phases:  []string{"clusters"},
				})
			}
		}
		for _, entry := range env.Spec.InfraComponents.Proxies {
			if entry.Connection == nil || entry.Connection.Auth == nil || entry.Connection.Auth.ProxyAuthRef.Name == "" {
				continue
			}
			out = append(out, secretRefRequirement{
				refName: entry.Connection.Auth.ProxyAuthRef.Name,
				label:   fmt.Sprintf("proxy %s proxyAuthRef", entry.Name),
				phases:  []string{"provider", "cluster"},
			})
		}
		if registries := env.Spec.Registries; registries != nil && registries.Mirror != nil && registries.Mirror.CredentialsRef.Name != "" {
			out = append(out, secretRefRequirement{
				refName: registries.Mirror.CredentialsRef.Name,
				label:   "registry mirror credentialsRef",
				phases:  []string{"provider", "clusters"},
			})
		}
		if registries := env.Spec.Registries; registries != nil && registries.Mirror != nil && registries.Mirror.TrustBundleRef.Name != "" {
			out = append(out, secretRefRequirement{
				refName: registries.Mirror.TrustBundleRef.Name,
				label:   "registry mirror trustBundleRef",
				phases:  []string{"clusters"},
			})
		}
	}

	for _, h := range state.Hosts {
		if h.Spec.SSH == nil || h.Spec.SSH.KeyRef.Name == "" {
			continue
		}
		if locality.IsControllerLocalHost(h, localPolicy) {
			continue
		}
		out = append(out, secretRefRequirement{
			refName: h.Spec.SSH.KeyRef.Name,
			label:   fmt.Sprintf("host %s keyRef", h.Metadata.Name),
			phases:  []string{"provider", "cluster", "clusters"},
			role:    secret.MaterialSSHPrivate,
		})
	}
	for _, p := range state.InfraProviders {
		for _, mp := range p.Spec.MachineProfiles {
			if l := mp.Libvirt; l != nil && l.BMCEmulationDefaults != nil && l.BMCEmulationDefaults.Auth != nil && l.BMCEmulationDefaults.Auth.CredentialRef.Name != "" {
				out = append(out, secretRefRequirement{
					refName: l.BMCEmulationDefaults.Auth.CredentialRef.Name,
					label:   fmt.Sprintf("provider %s machineProfiles[%s] bmcEmulationDefaults credentialRef", p.Metadata.Name, mp.Name),
					phases:  []string{"provider", "clusters"},
				})
			}
			if v := mp.VSphere; v != nil {
				for i, vc := range v.VCenters {
					if vc.CredentialsRef.Name == "" {
						continue
					}
					out = append(out, secretRefRequirement{
						refName: vc.CredentialsRef.Name,
						label:   fmt.Sprintf("provider %s machineProfiles[%s] vsphere vcenters[%d] credentialsRef", p.Metadata.Name, mp.Name, i),
						phases:  []string{"provider"},
					})
				}
			}
			if k := mp.KubeVirt; k != nil && k.KubeconfigRef != nil && k.KubeconfigRef.Name != "" {
				out = append(out, secretRefRequirement{
					refName: k.KubeconfigRef.Name,
					label:   fmt.Sprintf("provider %s machineProfiles[%s] kubevirt kubeconfigRef", p.Metadata.Name, mp.Name),
					phases:  []string{"cluster", "clusters"},
				})
			}
		}
		for _, m := range p.Spec.Machines {
			if m.BareMetal == nil || m.BareMetal.BMC.CredentialsRef.Name == "" {
				continue
			}
			out = append(out, secretRefRequirement{
				refName: m.BareMetal.BMC.CredentialsRef.Name,
				label:   fmt.Sprintf("provider %s machines[%s] baremetal bmc credentialsRef", p.Metadata.Name, m.Name),
				phases:  []string{"provider", "clusters"},
			})
		}
	}

	for _, cluster := range state.ContainerClusters {
		install := cluster.Spec.Install
		if install.PullSecretRef.Name != "" {
			out = append(out, secretRefRequirement{
				refName: install.PullSecretRef.Name,
				label:   cluster.Metadata.Name + " pullSecretRef",
				phases:  []string{"clusters"},
			})
		}
		if install.NodeSSH.KeyPairRef.Name != "" {
			out = append(out, secretRefRequirement{
				refName: install.NodeSSH.KeyPairRef.Name,
				label:   cluster.Metadata.Name + " nodeSSH keyPairRef",
				phases:  []string{"clusters"},
				role:    secret.MaterialSSHPublic,
				sshPair: true,
			})
		}
		if install.NodeSSH.PublicKeyRef.Name != "" {
			out = append(out, secretRefRequirement{
				refName: install.NodeSSH.PublicKeyRef.Name,
				label:   cluster.Metadata.Name + " nodeSSH publicKeyRef",
				phases:  []string{"clusters"},
				role:    secret.MaterialSSHPublic,
			})
		}
		if install.NodeSSH.PrivateKeyRef.Name != "" {
			out = append(out, secretRefRequirement{
				refName: install.NodeSSH.PrivateKeyRef.Name,
				label:   cluster.Metadata.Name + " nodeSSH privateKeyRef",
				phases:  []string{"clusters"},
				role:    secret.MaterialSSHPrivate,
			})
		}
		for i, ref := range install.AdditionalTrustBundleRefs {
			if ref.Name == "" {
				continue
			}
			out = append(out, secretRefRequirement{
				refName: ref.Name,
				label:   fmt.Sprintf("%s additionalTrustBundleRefs[%d]", cluster.Metadata.Name, i),
				phases:  []string{"clusters"},
			})
		}
		if serving := install.ServingCertificates; serving != nil {
			if api := serving.APIServer; api != nil {
				for i, cert := range api.NamedCertificates {
					if cert.SecretRef.Name == "" {
						continue
					}
					out = append(out, secretRefRequirement{
						refName: cert.SecretRef.Name,
						label:   fmt.Sprintf("%s apiServer namedCertificates[%d] secretRef", cluster.Metadata.Name, i),
						phases:  []string{"clusters"},
						tlsPair: true,
					})
				}
			}
			if ingress := serving.Ingress; ingress != nil && ingress.DefaultCertificateRef.Name != "" {
				out = append(out, secretRefRequirement{
					refName: ingress.DefaultCertificateRef.Name,
					label:   cluster.Metadata.Name + " ingress defaultCertificateRef",
					phases:  []string{"clusters"},
					tlsPair: true,
				})
			}
		}
	}
	return resolveSecretRequirementSources(state, out)
}

func environmentForChecks(state v1alpha1.State) *v1alpha1.Environment {
	if len(state.Environments) == 0 {
		return nil
	}
	return &state.Environments[0]
}

func resolveSecretRequirementSources(state v1alpha1.State, requirements []secretRefRequirement) []secretRefRequirement {
	declared := map[string]v1alpha1.EnvironmentSecretSpec{}
	env := primaryEnvironmentForSync(state)
	if env != nil {
		for name, spec := range env.Spec.Secrets {
			declared[name] = spec
		}
	}
	for i := range requirements {
		spec, ok := declared[requirements[i].refName]
		if !ok {
			requirements[i].source = secretRefSourceFile
			continue
		}
		switch {
		case spec.Generated != nil:
			requirements[i].source = secretRefSourceGenerated
			requirements[i].generatedKind = generatedSecretKind(spec)
		case spec.File == "" || env.Spec.SecretStorage.Mode == v1alpha1.SecretStorageModeContext:
			requirements[i].source = secretRefSourceContext
		default:
			requirements[i].source = secretRefSourceFile
		}
	}
	return requirements
}

func generatedSecretKind(spec v1alpha1.EnvironmentSecretSpec) string {
	if spec.Generated == nil {
		return ""
	}
	switch {
	case spec.Generated.Credentials != nil:
		return "credentials"
	case spec.Generated.SelfSignedCertificate != nil:
		return "selfSignedCertificate"
	case spec.Generated.SSHKeyPair != nil:
		return "sshKeyPair"
	default:
		return ""
	}
}

func tlsSecretFileChecks(req secretRefRequirement, env *v1alpha1.Environment, secretsDir string, deps preflightDeps) []preflightCheck {
	certPath := resolvedSecretPath(req.refName, env, secretsDir)
	keyPath := secret.ResolveTLSKeyPath(req.refName, env, secretsDir)
	return []preflightCheck{
		secretFileCheck(req.refName, certPath, req.label+" tls.crt", false, req.source == secretRefSourceContext || req.source == secretRefSourceGenerated, deps),
		secretFileCheck(req.refName, keyPath, req.label+" tls.key", false, req.source == secretRefSourceContext || req.source == secretRefSourceGenerated, deps),
	}
}

func generatedSelfSignedDriftChecks(state v1alpha1.State, secretsDir string) []preflightCheck {
	requests, err := generatedSelfSignedRequests(state)
	if err != nil {
		return []preflightCheck{{
			Group:       checkGroupSecretMaterial,
			Name:        "generated self-signed certificate requests",
			Status:      output.StatusFail,
			Evidence:    err.Error(),
			Impact:      "Generated certificate material cannot be validated",
			Remediation: "fix Environment.spec.secrets generated certificate requests",
		}}
	}
	var checks []preflightCheck
	for _, req := range requests {
		certPath := filepath.Join(secretsDir, req.name)
		keyPath := certPath + ".key"
		certExists, err := safefs.RegularFileExists(certPath)
		if err != nil {
			checks = append(checks, preflightCheck{
				Group:       checkGroupSecretMaterial,
				Name:        "generated self-signed certificate " + req.name,
				Status:      output.StatusFail,
				Evidence:    err.Error(),
				Impact:      "Generated certificate material cannot be inspected",
				Remediation: "fix file permissions or remove the generated certificate and rerun bootwright secret generate",
			})
			continue
		}
		if !certExists {
			continue
		}
		name := "generated self-signed certificate " + req.name
		if err := secret.VerifySelfSignedCertificateMatchesRequest(certPath, req.certificate); err != nil {
			checks = append(checks, preflightCheck{
				Group:       checkGroupSecretMaterial,
				Name:        name,
				Status:      output.StatusFail,
				Evidence:    err.Error(),
				Impact:      "Generated certificate on disk does not match desired state",
				Remediation: "remove " + certPath + " and " + keyPath + ", then run bootwright secret generate",
			})
			continue
		}
		checks = append(checks, preflightCheck{
			Group:    checkGroupSecretMaterial,
			Name:     name,
			Status:   output.StatusOK,
			Evidence: certPath,
		})
	}
	return checks
}

func defaultLookPath(name string, extraDirs []string) (string, error) {
	for _, dir := range extraDirs {
		candidate := filepath.Join(dir, name)
		info, err := os.Stat(candidate)
		if err != nil || info.IsDir() || info.Mode()&0o111 == 0 {
			continue
		}
		return candidate, nil
	}
	if path, ok, err := callerio.LookPath(name); ok {
		if err == nil {
			return path, nil
		}
		if !errors.Is(err, exec.ErrNotFound) {
			return "", err
		}
	}
	if path, err := exec.LookPath(name); err == nil {
		return path, nil
	}
	return "", exec.ErrNotFound
}
