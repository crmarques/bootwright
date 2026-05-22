package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/safefs"
	"github.com/crmarques/bootwright/internal/secret"
)

type secretRefRequirement struct {
	refName       string
	label         string
	phases        []string
	source        secretRefSource
	generatedKind string
	publicKey     bool
}

type secretRefSource string

const (
	secretRefSourceFile      secretRefSource = "file"
	secretRefSourceContext   secretRefSource = "context"
	secretRefSourceGenerated secretRefSource = "generated"
)

func secretRefChecks(state v1alpha1.State, secretsDir string, selected []Phase, deps preflightDeps) []preflightCheck {
	requirements := collectSecretRefRequirements(state)
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
		if req.source == secretRefSourceGenerated {
			path := filepath.Join(secretsDir, req.refName)
			checks = append(checks, generatedSecretCheck(req.refName, path, req.label, req.generatedKind, deps))
			continue
		}
		path := resolvedSecretPath(req.refName, env, secretsDir)
		checks = append(checks, secretFileCheck(req.refName, path, req.label, req.publicKey, req.source == secretRefSourceContext, deps))
	}
	return checks
}

func collectSecretRefRequirements(state v1alpha1.State) []secretRefRequirement {
	var out []secretRefRequirement

	if env := environmentForChecks(state); env != nil {
		if env.Spec.Proxy != nil && env.Spec.Proxy.Auth != nil && env.Spec.Proxy.Auth.ProxyAuthRef.Name != "" {
			out = append(out, secretRefRequirement{
				refName: env.Spec.Proxy.Auth.ProxyAuthRef.Name,
				label:   "proxy proxyAuthRef",
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
	}

	for _, h := range state.Hosts {
		if h.Spec.SSH == nil || h.Spec.SSH.KeyRef.Name == "" {
			continue
		}
		out = append(out, secretRefRequirement{
			refName: h.Spec.SSH.KeyRef.Name,
			label:   fmt.Sprintf("host %s sshKeyRef", h.Metadata.Name),
			phases:  []string{"provider", "cluster", "clusters"},
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
			if k := mp.KubeVirt; k != nil && k.ClusterRef.Name != "" {
				out = append(out, secretRefRequirement{
					refName: k.ClusterRef.Name,
					label:   fmt.Sprintf("provider %s machineProfiles[%s] kubevirt clusterRef", p.Metadata.Name, mp.Name),
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
		if install.SSHKeyRef.Name != "" {
			out = append(out, secretRefRequirement{
				refName:   install.SSHKeyRef.Name,
				label:     cluster.Metadata.Name + " sshKeyRef",
				phases:    []string{"clusters"},
				publicKey: true,
			})
		}
		if install.AdditionalTrustBundleRef.Name != "" {
			out = append(out, secretRefRequirement{
				refName: install.AdditionalTrustBundleRef.Name,
				label:   cluster.Metadata.Name + " additionalTrustBundleRef",
				phases:  []string{"clusters"},
			})
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
	if env := primaryEnvironmentForSync(state); env != nil {
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
		case spec.File == "":
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
	default:
		return ""
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
	if path, err := exec.LookPath(name); err == nil {
		return path, nil
	}
	return "", exec.ErrNotFound
}
