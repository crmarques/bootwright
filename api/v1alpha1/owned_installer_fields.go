package v1alpha1

type OwnedInstallerFields struct {
	InstallConfigKeys []string

	InstallConfigPaths []string

	AgentConfigKeys []string
}

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
