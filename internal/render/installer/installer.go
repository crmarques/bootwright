package installer

import (
	"path/filepath"

	"github.com/crmarques/bootwright/api/v1alpha1"
	stateview "github.com/crmarques/bootwright/internal/state/view"
)

const (
	InstallerRelativeDir = "installer"

	RuntimeRelativeDir = "installer"
)

type InstallerAsset struct {
	ClusterName                  string
	ClusterDir                   string
	Dir                          string
	InstallConfigPath            string
	AgentConfigPath              string
	InstallManifestsDir          string
	WorkDir                      string
	ClusterSecretsDir            string
	EffectiveInstallConfigPath   string
	EffectiveAgentConfigPath     string
	EffectiveInstallManifestsDir string
}

func InstallerAssets(clustersDir string, state v1alpha1.State) []InstallerAsset {
	assets := make([]InstallerAsset, 0, len(state.ContainerClusters))
	for _, ocp := range state.ContainerClusters {
		clusterDir := filepath.Join(clustersDir, ocp.Metadata.Name)
		dir := filepath.Join(clusterDir, "rendered", InstallerRelativeDir)
		workDir := filepath.Join(clusterDir, "runtime", RuntimeRelativeDir)
		assets = append(assets, InstallerAsset{
			ClusterName:                  ocp.Metadata.Name,
			ClusterDir:                   clusterDir,
			Dir:                          dir,
			InstallConfigPath:            filepath.Join(dir, "install-config.yaml"),
			AgentConfigPath:              filepath.Join(dir, "agent-config.yaml"),
			InstallManifestsDir:          filepath.Join(dir, "openshift"),
			WorkDir:                      workDir,
			ClusterSecretsDir:            filepath.Join(clusterDir, "secrets"),
			EffectiveInstallConfigPath:   filepath.Join(workDir, "install-config.yaml"),
			EffectiveAgentConfigPath:     filepath.Join(workDir, "agent-config.yaml"),
			EffectiveInstallManifestsDir: filepath.Join(workDir, "openshift"),
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
			ClusterName:                  ocp.Metadata.Name,
			Dir:                          dir,
			InstallConfigPath:            installConfigPath,
			AgentConfigPath:              agentConfigPath,
			InstallManifestsDir:          filepath.Join(dir, "openshift"),
			WorkDir:                      dir,
			EffectiveInstallConfigPath:   installConfigPath,
			EffectiveAgentConfigPath:     agentConfigPath,
			EffectiveInstallManifestsDir: filepath.Join(dir, "openshift"),
		})
	}
	return assets
}

func InstallerConfig(state v1alpha1.State, ocp v1alpha1.ContainerCluster) (map[string]any, error) {
	return InstallerConfigWithSecrets(state, ocp, PlaceholderInstallerSecrets(state, ocp))
}

func InstallerConfigWithSecrets(state v1alpha1.State, ocp v1alpha1.ContainerCluster, secrets InstallerSecrets) (map[string]any, error) {
	ci, err := ClusterInstallForOCP(state, ocp)
	if err != nil {
		return nil, err
	}
	platformKind := clusterPlatformKind(ci, ocp)
	env := stateview.Environment(state)
	base := map[string]any{
		"apiVersion": "v1",
		"baseDomain": EnvironmentBaseDomain(env),
		"metadata":   map[string]any{"name": ocp.Metadata.Name},
		"compute": []any{
			map[string]any{
				"name":     "worker",
				"replicas": computeReplicaCount(ocp),
			},
		},
		"controlPlane": map[string]any{
			"name":     "master",
			"replicas": nodeRoleCount(ocp, v1alpha1.NodeRoleMaster),
		},
		"networking": networkingConfig(state, ci, ocp),
		"platform":   platformConfig(state, platformKind, ci, ocp, secrets),
		"pullSecret": secrets.PullSecret,
		"sshKey":     secrets.SSHKey,
	}
	if ocp.Spec.Security.FIPS.Enabled {
		base["fips"] = true
	}
	if secrets.TrustBundle != "" {
		base["additionalTrustBundle"] = secrets.TrustBundle
		base["additionalTrustBundlePolicy"] = "Always"
	}
	if mirrors := imageDigestSourcesConfig(installerImageDigestSources(state, ci, ocp, env)); len(mirrors) > 0 {
		base["imageDigestSources"] = mirrors
	}
	eff, managedURL, err := clusterInstallProxyInputs(state, env, ci)
	if err != nil {
		return nil, err
	}
	if pc := installerProxyConfig(eff, secrets, managedURL); pc != nil {
		base["proxy"] = pc
	}
	return base, nil
}

func AgentConfig(state v1alpha1.State, ocp v1alpha1.ContainerCluster) (map[string]any, error) {
	ci, err := ClusterInstallForOCP(state, ocp)
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
	for key, value := range disconnectedBootArtifactsConfig(state, ocp) {
		base[key] = value
	}
	if sources := stateview.ResolvedAdditionalNTPSources(state); len(sources) > 0 {
		ntp := make([]any, 0, len(sources))
		for _, source := range sources {
			ntp = append(ntp, source)
		}
		base["additionalNTPSources"] = ntp
	}
	return base, nil
}

func EnvironmentBaseDomain(env *v1alpha1.Environment) string {
	if env == nil {
		return ""
	}
	return env.Spec.Domains.ContainerClustersDomain()
}
