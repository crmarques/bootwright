package desiredstate

import (
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func validateEnvironments(state v1alpha1.State) []string {
	var errs []string
	envs := state.Environments
	switch {
	case len(envs) == 0:
		errs = append(errs, "exactly one Environment is required in the loaded state (got 0)")
	case len(envs) > 1:
		names := make([]string, 0, len(envs))
		for _, e := range envs {
			names = append(names, e.Metadata.Name)
		}
		errs = append(errs, fmt.Sprintf("exactly one Environment is required in the loaded state (got %d: %s)", len(envs), strings.Join(names, ", ")))
	}
	seen := map[string]bool{}
	for _, env := range envs {
		if e := validateName(v1alpha1.KindEnvironment, env.Metadata.Name); e != "" {
			errs = append(errs, e)
			continue
		}
		if seen[env.Metadata.Name] {
			errs = append(errs, fmt.Sprintf("duplicate Environment %q", env.Metadata.Name))
		}
		seen[env.Metadata.Name] = true
		if env.Spec.BaseDomain == "" {
			errs = append(errs, fmt.Sprintf("Environment/%s spec.baseDomain is required", env.Metadata.Name))
		}
		errs = append(errs, validateEnvironmentSafety(env)...)
		errs = append(errs, validateEnvironmentDefaults(env)...)
		errs = append(errs, validateEnvironmentSecretStorage(env)...)
		errs = append(errs, validateEnvironmentResources(env)...)
		errs = append(errs, validateEnvironmentContainerClusters(env, state)...)
		errs = append(errs, validateEnvironmentStorageClusters(env, state)...)
		errs = append(errs, validateEnvironmentInfraComponents(env, state)...)
		errs = append(errs, validateEnvironmentRegistries(env)...)
		errs = append(errs, validateEnvironmentInstallTrust(env)...)
		errs = append(errs, validateComponentImages(env)...)
	}
	return errs
}

func validateEnvironmentSafety(env v1alpha1.Environment) []string {
	var errs []string
	switch env.Spec.Safety.DestroyProtection {
	case "", v1alpha1.EnvironmentDestroyProtectionAllow, v1alpha1.EnvironmentDestroyProtectionRequiredOverride:
	default:
		errs = append(errs, fmt.Sprintf("Environment/%s spec.safety.destroyProtection %q must be one of {%s, %s}",
			env.Metadata.Name,
			env.Spec.Safety.DestroyProtection,
			v1alpha1.EnvironmentDestroyProtectionAllow,
			v1alpha1.EnvironmentDestroyProtectionRequiredOverride))
	}
	for _, kind := range env.Spec.Safety.ProtectedKinds {
		switch kind {
		case v1alpha1.KindContainerCluster, v1alpha1.KindStorageCluster, v1alpha1.KindMachine:
		default:
			errs = append(errs, fmt.Sprintf("Environment/%s spec.safety.protectedKinds %q must be one of {%s, %s, %s}",
				env.Metadata.Name, kind,
				v1alpha1.KindContainerCluster, v1alpha1.KindStorageCluster, v1alpha1.KindMachine))
		}
	}
	return errs
}

func validateEnvironmentDefaults(env v1alpha1.Environment) []string {
	var errs []string
	if ref := env.Spec.Defaults.ArtifactServerRef.Name; ref != "" {
		if _, ok := environmentArtifactServerByName(&env, ref); !ok {
			errs = append(errs, fmt.Sprintf("Environment/%s spec.defaults.artifactServerRef %q does not resolve to spec.infraComponents.artifactServers[].name", env.Metadata.Name, ref))
		}
	}
	if mirror := strings.TrimSpace(env.Spec.Defaults.ClientsMirror); mirror != "" && !isHTTPURL(mirror) {
		errs = append(errs, fmt.Sprintf("Environment/%s spec.defaults.clientsMirror %q must be an http(s) URL", env.Metadata.Name, env.Spec.Defaults.ClientsMirror))
	}
	if mirror := strings.TrimSpace(env.Spec.Defaults.VirtctlMirror); mirror != "" && !isHTTPURL(mirror) {
		errs = append(errs, fmt.Sprintf("Environment/%s spec.defaults.virtctlMirror %q must be an http(s) URL", env.Metadata.Name, env.Spec.Defaults.VirtctlMirror))
	}
	errs = append(errs, validateNodeSSHSpec(
		fmt.Sprintf("Environment/%s spec.defaults.install.nodeSSH", env.Metadata.Name),
		env.Spec.Defaults.Install.NodeSSH,
		false,
	)...)
	return errs
}

func validateEnvironmentContainerClusters(env v1alpha1.Environment, state v1alpha1.State) []string {
	var errs []string
	// An authored empty list (present but with no entries) is rejected: it reads
	// as "select nothing" but is treated as "select all", silently widening
	// apply/destroy scope to the whole fleet. Omit the field to select all.
	if env.Spec.ContainerClusters != nil && len(env.Spec.ContainerClusters) == 0 {
		errs = append(errs, fmt.Sprintf("Environment/%s spec.containerClusters is an empty list; omit it to select all clusters, or list the clusters to select", env.Metadata.Name))
	}
	known := map[string]bool{}
	for _, cluster := range state.ContainerClusters {
		known[cluster.Metadata.Name] = true
	}
	seen := map[string]bool{}
	for i, name := range env.Spec.ContainerClusters {
		owner := fmt.Sprintf("Environment/%s spec.containerClusters[%d]", env.Metadata.Name, i)
		if name == "" {
			errs = append(errs, owner+" must not be empty")
			continue
		}
		if seen[name] {
			errs = append(errs, fmt.Sprintf("%s %q is duplicated", owner, name))
			continue
		}
		seen[name] = true
		if !known[name] {
			errs = append(errs, fmt.Sprintf("%s %q does not match any ContainerCluster", owner, name))
		}
	}
	return errs
}

func validateEnvironmentStorageClusters(env v1alpha1.Environment, state v1alpha1.State) []string {
	var errs []string
	if env.Spec.StorageClusters != nil && len(env.Spec.StorageClusters) == 0 {
		errs = append(errs, fmt.Sprintf("Environment/%s spec.storageClusters is an empty list; omit it to select all clusters, or list the clusters to select", env.Metadata.Name))
	}
	known := map[string]bool{}
	for _, cluster := range state.StorageClusters {
		known[cluster.Metadata.Name] = true
	}
	seen := map[string]bool{}
	for i, name := range env.Spec.StorageClusters {
		owner := fmt.Sprintf("Environment/%s spec.storageClusters[%d]", env.Metadata.Name, i)
		if name == "" {
			errs = append(errs, owner+" must not be empty")
			continue
		}
		if seen[name] {
			errs = append(errs, fmt.Sprintf("%s %q is duplicated", owner, name))
			continue
		}
		seen[name] = true
		if !known[name] {
			errs = append(errs, fmt.Sprintf("%s %q does not match any StorageCluster", owner, name))
		}
	}
	return errs
}

func validateEnvironmentResources(env v1alpha1.Environment) []string {
	if env.Spec.Resources == nil {
		return nil
	}
	if len(env.Spec.Resources) == 0 {
		return []string{fmt.Sprintf("Environment/%s spec.resources must include at least one file or directory when set", env.Metadata.Name)}
	}
	// The path-shape rules (empty, whitespace, absolute, directory escape) are
	// enforced at load by resolveEnvironmentResourcePath, which aborts before
	// Validate runs; duplicating them here only produced dead arms whose nicely
	// routed findings never rendered. The cross-entry duplicate check is the one
	// rule load does not cover, so it is all that remains reachable here.
	var errs []string
	seen := map[string]bool{}
	for i, value := range env.Spec.Resources {
		owner := fmt.Sprintf("Environment/%s spec.resources[%d]", env.Metadata.Name, i)
		clean := filepath.Clean(value)
		if seen[clean] {
			errs = append(errs, fmt.Sprintf("%s %q is a duplicate", owner, value))
			continue
		}
		seen[clean] = true
	}
	return errs
}

func validateProxyURL(raw string) error {
	return validateHTTPURL(raw)
}

func validateHTTPURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return err
	}
	if u.Scheme == "" || u.Host == "" {
		return errors.New("must include scheme and host")
	}
	switch u.Scheme {
	case "http", "https":
	default:
		return fmt.Errorf("scheme must be http or https, got %q", u.Scheme)
	}
	if u.Hostname() == "" {
		return errors.New("must include host")
	}
	return nil
}

func proxyURLHasInlineCredentials(url string) bool {
	idx := strings.Index(url, "://")
	if idx < 0 {
		return false
	}
	authority := url[idx+3:]
	if at := strings.Index(authority, "@"); at >= 0 {
		if slash := strings.Index(authority, "/"); slash < 0 || at < slash {
			return true
		}
	}
	return false
}
