package v1alpha1

// OwnedInstallerFields is the renderer contract audit registry for fields
// Bootwright writes into generated install-config.yaml and agent-config.yaml.
//
// When the renderer learns to write a new field, list it here.
// When it stops owning a field, remove it from here.
type OwnedInstallerFields struct {
	// InstallConfigKeys are top-level keys in install-config.yaml that
	// Bootwright derives.
	InstallConfigKeys []string

	// InstallConfigPaths are dotted paths inside install-config.yaml
	// that Bootwright derives.
	InstallConfigPaths []string

	// AgentConfigKeys are top-level keys in agent-config.yaml that
	// Bootwright derives.
	AgentConfigKeys []string
}

// OwnedFields returns the renderer's owned-field audit set. It is a test-time
// drift guard (compared against rendered output by render/owned_test.go), not a
// runtime input the validator or renderer reads. Callers MUST treat the returned
// slices as read-only.
func OwnedFields() OwnedInstallerFields {
	return OwnedInstallerFields{
		InstallConfigKeys: []string{
			"apiVersion",
			"metadata",
			"baseDomain",
			"pullSecret",
			"sshKey",
			"additionalTrustBundle",
			"additionalTrustBundlePolicy",
			"fips",
			"controlPlane",
			"compute",
			"imageDigestSources",
			"proxy",
		},
		InstallConfigPaths: []string{
			"networking.machineNetwork",
			"networking.networkType",
			"networking.clusterNetwork",
			"networking.serviceNetwork",
			"platform.baremetal.apiVIPs",
			"platform.baremetal.ingressVIPs",
			"platform.baremetal.loadBalancer",
			"platform.baremetal.provisioningNetwork",
			"platform.external",
			"platform.none",
			"platform.vsphere.apiVIPs",
			"platform.vsphere.ingressVIPs",
			"platform.vsphere.vcenters",
			"platform.vsphere.failureDomains",
			"platform.vsphere.nodeNetworking",
		},
		AgentConfigKeys: []string{
			"apiVersion",
			"kind",
			"metadata",
			"rendezvousIP",
			"hosts",
			"minimalISO",
			"bootArtifactsBaseURL",
			"additionalNTPSources",
		},
	}
}
