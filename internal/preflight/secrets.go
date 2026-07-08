package preflight

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/host/callerio"
	"github.com/crmarques/bootwright/internal/host/execution"
	"github.com/crmarques/bootwright/internal/host/safefs"
	"github.com/crmarques/bootwright/internal/infra/locality"
	"github.com/crmarques/bootwright/internal/secrets"
	stateview "github.com/crmarques/bootwright/internal/state/view"
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
	// owner ties the requirement to the work object whose lifecycle needs it (see
	// secretRefOwner in scope.go), so a scoped run can drop secrets owned by
	// render-reference pull-ins.
	owner secretRefOwner
}

type secretRefSource string

const (
	secretRefSourceFile      secretRefSource = "file"
	secretRefSourceContext   secretRefSource = "context"
	secretRefSourceGenerated secretRefSource = "generated"
)

func secretRefChecksWithLocalityPolicy(state v1alpha1.State, secretsDir string, selected []Phase, deps Deps, localPolicy locality.Policy, secretScope *SecretScope) []Check {
	requirements := collectSecretRefRequirementsWithLocalityPolicy(state, localPolicy)
	var inScope []secretRefRequirement
	needsSecretsDir := false
	for _, req := range requirements {
		if !anyPhaseInScope(req.phases, selected) {
			continue
		}
		if !secretScope.allowsMachine(req.owner.machine) || !secretScope.allowsStorageCluster(req.owner.storageCluster) {
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
	idx := secret.NewIndex(state)
	var checks []Check
	if needsSecretsDir {
		checks = append(checks, secretsDirCheck(secretsDir, deps))
	}
	for _, req := range inScope {
		if req.tlsPair {
			checks = append(checks, tlsSecretFileChecks(req, idx, secretsDir, deps)...)
			continue
		}
		if req.sshPair {
			checks = append(checks, sshKeyPairFileChecks(req, idx, secretsDir, deps)...)
			continue
		}
		if req.source == secretRefSourceGenerated && req.generatedKind == "sshKeyPair" {
			checks = append(checks, generatedSSHKeyPairChecks(req, idx, secretsDir, deps)...)
			continue
		}
		if req.source == secretRefSourceGenerated {
			path := filepath.Join(secretsDir, req.refName)
			checks = append(checks, generatedSecretCheck(req.refName, path, req.label, req.generatedKind, deps))
			continue
		}
		path := secret.ResolveMaterialPath(req.refName, idx, secretsDir, req.role)
		checks = append(checks, secretFileCheck(req.refName, path, req.label, req.role == secret.MaterialSSHPublic, req.source == secretRefSourceContext, secret.MaterialPathUsesExternalSource(req.refName, idx, req.role), deps))
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
					phases:  []string{"deps", "base"},
				})
			}
		}
		for _, entry := range env.Spec.InfraComponents.Proxies {
			if entry.Connection == nil {
				continue
			}
			if entry.Connection.Auth != nil && entry.Connection.Auth.ProxyAuthRef.Name != "" {
				out = append(out, secretRefRequirement{
					refName: entry.Connection.Auth.ProxyAuthRef.Name,
					label:   fmt.Sprintf("proxy %s proxyAuthRef", entry.Name),
					phases:  []string{"fabric", "machines"},
				})
			}
			if entry.Connection.TrustBundleRef.Name != "" {
				out = append(out, secretRefRequirement{
					refName: entry.Connection.TrustBundleRef.Name,
					label:   fmt.Sprintf("proxy %s trustBundleRef", entry.Name),
					phases:  []string{"fabric", "machines"},
				})
			}
		}
		if registries := env.Spec.Registries; registries != nil && registries.Mirror != nil && registries.Mirror.CredentialsRef.Name != "" {
			out = append(out, secretRefRequirement{
				refName: registries.Mirror.CredentialsRef.Name,
				label:   "registry mirror credentialsRef",
				phases:  []string{"fabric", "deps", "base"},
			})
		}
		if registries := env.Spec.Registries; registries != nil && registries.Mirror != nil && registries.Mirror.TrustBundleRef.Name != "" {
			out = append(out, secretRefRequirement{
				refName: registries.Mirror.TrustBundleRef.Name,
				label:   "registry mirror trustBundleRef",
				phases:  []string{"deps", "base"},
			})
		}
		out = append(out, collectEntitlementSecretRefRequirements(state)...)
	}

	for _, machine := range state.Machines {
		if machine.Spec.Access.SSH == nil || machine.Spec.Access.SSH.KeyRef.Name == "" {
			continue
		}
		if locality.IsControllerLocalMachine(machine, localPolicy) {
			continue
		}
		out = append(out, machineSSHSecretRequirements(fmt.Sprintf("machine %s", machine.Metadata.Name), []string{"fabric", "machines", "deps", "base"}, machine, false, secretRefOwner{machine: machine.Metadata.Name})...)
	}
	for _, p := range state.InfraProviders {
		if l := p.Spec.Libvirt; l != nil && l.BMCEmulationDefaults != nil && l.BMCEmulationDefaults.Auth != nil && l.BMCEmulationDefaults.Auth.CredentialsRef.Name != "" {
			out = append(out, secretRefRequirement{
				refName: l.BMCEmulationDefaults.Auth.CredentialsRef.Name,
				label:   fmt.Sprintf("provider %s libvirt bmcEmulationDefaults credentialsRef", p.Metadata.Name),
				phases:  []string{"fabric", "deps", "base"},
			})
		}
		if v := p.Spec.VSphere; v != nil {
			for i, vc := range v.VCenters {
				if vc.CredentialsRef.Name == "" {
					continue
				}
				out = append(out, secretRefRequirement{
					refName: vc.CredentialsRef.Name,
					label:   fmt.Sprintf("provider %s vsphere vcenters[%d] credentialsRef", p.Metadata.Name, i),
					phases:  []string{"fabric", "machines", "deps", "base"},
				})
			}
		}
		if k := p.Spec.KubeVirt; k != nil && k.KubeconfigRef != nil && k.KubeconfigRef.Name != "" {
			out = append(out, secretRefRequirement{
				refName: k.KubeconfigRef.Name,
				label:   fmt.Sprintf("provider %s kubevirt kubeconfigRef", p.Metadata.Name),
				phases:  []string{"machines", "deps", "base"},
			})
		}
	}
	for _, machine := range state.Machines {
		if machine.Spec.Hardware.Management.BMC.CredentialsRef.Name == "" {
			continue
		}
		out = append(out, secretRefRequirement{
			refName: machine.Spec.Hardware.Management.BMC.CredentialsRef.Name,
			label:   fmt.Sprintf("machine %s hardware management bmc credentialsRef", machine.Metadata.Name),
			phases:  []string{"fabric", "deps", "base"},
			owner:   secretRefOwner{machine: machine.Metadata.Name},
		})
	}

	for _, cluster := range state.ContainerClusters {
		install := cluster.Spec.Install
		if install.PullSecretRef.Name != "" {
			out = append(out, secretRefRequirement{
				refName: install.PullSecretRef.Name,
				label:   cluster.Metadata.Name + " pullSecretRef",
				phases:  []string{"deps", "base"},
			})
		}
		if install.NodeSSH.KeyPairRef.Name != "" {
			out = append(out, secretRefRequirement{
				refName: install.NodeSSH.KeyPairRef.Name,
				label:   cluster.Metadata.Name + " nodeSSH keyPairRef",
				phases:  []string{"deps", "base"},
				role:    secret.MaterialSSHPublic,
				sshPair: true,
			})
		}
		if install.NodeSSH.PublicKeyRef.Name != "" {
			out = append(out, secretRefRequirement{
				refName: install.NodeSSH.PublicKeyRef.Name,
				label:   cluster.Metadata.Name + " nodeSSH publicKeyRef",
				phases:  []string{"deps", "base"},
				role:    secret.MaterialSSHPublic,
			})
		}
		if install.NodeSSH.PrivateKeyRef.Name != "" {
			out = append(out, secretRefRequirement{
				refName: install.NodeSSH.PrivateKeyRef.Name,
				label:   cluster.Metadata.Name + " nodeSSH privateKeyRef",
				phases:  []string{"deps", "base"},
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
				phases:  []string{"deps", "base"},
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
						phases:  []string{"deps", "base"},
						tlsPair: true,
					})
				}
			}
			if ingress := serving.Ingress; ingress != nil && ingress.DefaultCertificateRef.Name != "" {
				out = append(out, secretRefRequirement{
					refName: ingress.DefaultCertificateRef.Name,
					label:   cluster.Metadata.Name + " ingress defaultCertificateRef",
					phases:  []string{"deps", "base"},
					tlsPair: true,
				})
			}
		}
	}
	out = append(out, collectStorageSecretRefRequirements(state)...)
	return resolveSecretRequirementSources(state, out)
}

func environmentForChecks(state v1alpha1.State) *v1alpha1.Environment {
	if len(state.Environments) == 0 {
		return nil
	}
	return &state.Environments[0]
}

func resolveSecretRequirementSources(state v1alpha1.State, requirements []secretRefRequirement) []secretRefRequirement {
	mode := ""
	if env := environmentForChecks(state); env != nil {
		mode = env.Spec.SecretStorage.Mode
	}
	for i := range requirements {
		s, ok := stateview.Secret(state, requirements[i].refName)
		if !ok {
			requirements[i].source = secretRefSourceFile
			continue
		}
		src := s.Spec.Source
		switch {
		case src.Generated != nil:
			requirements[i].source = secretRefSourceGenerated
			requirements[i].generatedKind = generatedSecretKind(s)
		case src.File == nil || mode == v1alpha1.SecretStorageModeContext:
			requirements[i].source = secretRefSourceContext
		default:
			requirements[i].source = secretRefSourceFile
		}
	}
	return requirements
}

// generatedSecretKind maps a generated Secret's type onto the legacy generated
// kind labels the preflight material checks branch on.
func generatedSecretKind(s v1alpha1.Secret) string {
	if s.Spec.Source.Generated == nil {
		return ""
	}
	switch s.Spec.Type {
	case v1alpha1.SecretTypeUsernamePassword:
		return "credentials"
	case v1alpha1.SecretTypeTLSCertificate:
		return "selfSignedCertificate"
	case v1alpha1.SecretTypeSSHKeyPair:
		return "sshKeyPair"
	case v1alpha1.SecretTypeToken:
		return "token"
	default:
		return ""
	}
}

func tlsSecretFileChecks(req secretRefRequirement, idx secret.Index, secretsDir string, deps Deps) []Check {
	certPath := secret.ResolvePath(req.refName, idx, secretsDir)
	keyPath := secret.ResolveTLSKeyPath(req.refName, idx, secretsDir)
	return []Check{
		secretFileCheck(req.refName, certPath, req.label+" tls.crt", false, req.source == secretRefSourceContext || req.source == secretRefSourceGenerated, secret.MaterialPathUsesExternalSource(req.refName, idx, secret.MaterialPrimary), deps),
		secretFileCheck(req.refName, keyPath, req.label+" tls.key", false, req.source == secretRefSourceContext || req.source == secretRefSourceGenerated, secret.MaterialPathUsesExternalSource(req.refName, idx, secret.MaterialTLSKey), deps),
	}
}

func generatedSelfSignedDriftChecks(state v1alpha1.State, secretsDir string) []Check {
	requests, err := secret.GeneratedSelfSignedRequests(state)
	if err != nil {
		return []Check{{
			Group:       checkGroupSecretMaterial,
			Name:        "generated self-signed certificate requests",
			Status:      StatusFail,
			Evidence:    err.Error(),
			Impact:      "Generated certificate material cannot be validated",
			Remediation: "fix Environment.spec.secrets generated certificate requests",
		}}
	}
	var checks []Check
	for _, req := range requests {
		certPath := filepath.Join(secretsDir, req.Name)
		keyPath := certPath + ".key"
		certExists, err := safefs.RegularFileExists(certPath)
		if err != nil {
			checks = append(checks, Check{
				Group:       checkGroupSecretMaterial,
				Name:        "generated self-signed certificate " + req.Name,
				Status:      StatusFail,
				Evidence:    err.Error(),
				Impact:      "Generated certificate material cannot be inspected",
				Remediation: "fix file permissions or remove the generated certificate and rerun bootwright secret generate",
			})
			continue
		}
		if !certExists {
			continue
		}
		name := "generated self-signed certificate " + req.Name
		if err := secret.VerifySelfSignedCertificateMatchesRequest(certPath, req.Certificate); err != nil {
			checks = append(checks, Check{
				Group:       checkGroupSecretMaterial,
				Name:        name,
				Status:      StatusFail,
				Evidence:    err.Error(),
				Impact:      "Generated certificate on disk does not match desired state",
				Remediation: "run bootwright secret generate --renew to regenerate, or remove " + certPath + " and " + keyPath + " then run bootwright secret generate",
			})
			continue
		}
		checks = append(checks, Check{
			Group:    checkGroupSecretMaterial,
			Name:     name,
			Status:   StatusOK,
			Evidence: certPath,
		})
	}
	return checks
}

func DefaultLookPath(name string, extraDirs []string) (string, error) {
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
		if !execution.IsNotFound(err) {
			return "", err
		}
	}
	path, err := execution.LookPath(name)
	if err != nil {
		return "", err
	}
	return path, nil
}
