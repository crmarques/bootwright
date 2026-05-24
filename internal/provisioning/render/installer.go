package render

import (
	"path/filepath"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

const (
	// InstallerRelativeDir is the path under <state-dir> for generated
	// installer placeholder artifacts.
	InstallerRelativeDir = "installer"

	// RuntimeRelativeDir is the path under <state-dir> for local-only
	// runtime artifacts.
	RuntimeRelativeDir = "runtime"
)

// InstallerAsset is the per-cluster path pair for the placeholder
// (`Dir`) and effective (`WorkDir`) install-config / agent-config
// outputs.
type InstallerAsset struct {
	ClusterName                string
	Method                     string
	Dir                        string
	InstallConfigPath          string
	AgentConfigPath            string
	WorkDir                    string
	EffectiveInstallConfigPath string
	EffectiveAgentConfigPath   string
}

func InstallerAssets(stateDir string, state v1alpha1.State) []InstallerAsset {
	assets := make([]InstallerAsset, 0, len(state.ContainerClusters))
	for _, ocp := range state.ContainerClusters {
		dir := filepath.Join(stateDir, InstallerRelativeDir, ocp.Metadata.Name)
		workDir := filepath.Join(stateDir, RuntimeRelativeDir, ocp.Metadata.Name, "installer")
		assets = append(assets, InstallerAsset{
			ClusterName:                ocp.Metadata.Name,
			Method:                     ocp.Spec.Install.Method,
			Dir:                        dir,
			InstallConfigPath:          filepath.Join(dir, "install-config.yaml"),
			AgentConfigPath:            filepath.Join(dir, "agent-config.yaml"),
			WorkDir:                    workDir,
			EffectiveInstallConfigPath: filepath.Join(workDir, "install-config.yaml"),
			EffectiveAgentConfigPath:   filepath.Join(workDir, "agent-config.yaml"),
		})
	}
	return assets
}

func InstallerToolInputAssets(outputDir string, state v1alpha1.State) []InstallerAsset {
	assets := make([]InstallerAsset, 0, len(state.ContainerClusters))
	for _, ocp := range state.ContainerClusters {
		dir := filepath.Join(outputDir, "openshift-install", ocp.Metadata.Name)
		installConfigPath := filepath.Join(dir, "install-config.yaml")
		agentConfigPath := filepath.Join(dir, "agent-config.yaml")
		assets = append(assets, InstallerAsset{
			ClusterName:                ocp.Metadata.Name,
			Method:                     ocp.Spec.Install.Method,
			Dir:                        dir,
			InstallConfigPath:          installConfigPath,
			AgentConfigPath:            agentConfigPath,
			WorkDir:                    dir,
			EffectiveInstallConfigPath: installConfigPath,
			EffectiveAgentConfigPath:   agentConfigPath,
		})
	}
	return assets
}

// InstallerConfig renders the placeholder install-config.yaml with
// secret references rather than secret material.
func InstallerConfig(state v1alpha1.State, ocp v1alpha1.ContainerCluster) (map[string]any, error) {
	return InstallerConfigWithSecrets(state, ocp, PlaceholderInstallerSecrets(ocp))
}

// InstallerConfigWithSecrets is the same as InstallerConfig but accepts
// resolved secret material so the result can be passed straight to
// openshift-install. ResolveInstaller calls this; the placeholder
// render path calls InstallerConfig.
func InstallerConfigWithSecrets(state v1alpha1.State, ocp v1alpha1.ContainerCluster, secrets InstallerSecrets) (map[string]any, error) {
	ci, err := clusterInfraForOCP(state, ocp)
	if err != nil {
		return nil, err
	}
	platformKind := clusterPlatformKind(ci, ocp)
	env := primaryEnvironment(state)
	base := map[string]any{
		"apiVersion": "v1",
		"baseDomain": environmentBaseDomain(env),
		"metadata":   map[string]any{"name": ocp.Metadata.Name},
		"compute": []any{
			map[string]any{
				"name":     "worker",
				"replicas": nodeRoleCount(ocp, v1alpha1.NodeRoleWorker),
			},
		},
		"controlPlane": map[string]any{
			"name":     "master",
			"replicas": nodeRoleCount(ocp, v1alpha1.NodeRoleMaster),
		},
		"networking": networkingConfig(state, ci, ocp),
		"platform":   platformConfig(state, platformKind, ci, ocp),
		"pullSecret": secrets.PullSecret,
		"sshKey":     secrets.SSHKey,
	}
	if secrets.TrustBundle != "" {
		base["additionalTrustBundle"] = secrets.TrustBundle
		base["additionalTrustBundlePolicy"] = "Always"
	}
	if mirrors := imageDigestSourcesConfig(installerImageDigestSources(state, ci, ocp, env)); len(mirrors) > 0 {
		base["imageDigestSources"] = mirrors
	}
	eff, managedURL := clusterInstallProxyInputs(state, env, ci)
	if pc := installerProxyConfig(eff, secrets, managedURL); pc != nil {
		base["proxy"] = pc
	}
	return base, nil
}

// AgentConfig renders the agent-config.yaml for the OpenShift agent
// installer. minimalISO + bootArtifactsBaseURL are auto-added in
// disconnected mode when a provider artifact publisher is available.
func AgentConfig(state v1alpha1.State, ocp v1alpha1.ContainerCluster) (map[string]any, error) {
	ci, err := clusterInfraForOCP(state, ocp)
	if err != nil {
		return nil, err
	}
	hosts, rendezvousIP := agentHosts(state, ci, ocp)
	base := map[string]any{
		"apiVersion":   "v1beta1",
		"kind":         "AgentConfig",
		"metadata":     map[string]any{"name": ocp.Metadata.Name},
		"rendezvousIP": rendezvousIP,
		"hosts":        hosts,
	}
	for key, value := range disconnectedBootArtifactsConfig(state, ci, ocp) {
		base[key] = value
	}
	if env := primaryEnvironment(state); env != nil && len(env.Spec.NTPSources) > 0 {
		ntp := make([]any, 0, len(env.Spec.NTPSources))
		for _, s := range env.Spec.NTPSources {
			ntp = append(ntp, s)
		}
		base["additionalNTPSources"] = ntp
	}
	return base, nil
}

func environmentBaseDomain(env *v1alpha1.Environment) string {
	if env == nil {
		return ""
	}
	return env.Spec.BaseDomain
}
