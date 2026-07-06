package installer

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	secretstore "github.com/crmarques/bootwright/internal/secrets"
)

func vsphereInstallerTestState() v1alpha1.State {
	provider := v1alpha1.InfraProvider{
		Metadata: v1alpha1.Metadata{Name: "lab-vsphere"},
		Spec: v1alpha1.InfraProviderSpec{
			Type: v1alpha1.ProvisionerVSphere,
			VSphere: &v1alpha1.InfraProviderVSphere{
				VCenters: []v1alpha1.VSphereVCenter{{
					Server:         "vcenter.example.test",
					Datacenters:    []string{"dc1"},
					CredentialsRef: v1alpha1.SecretRef{Name: "vcenter-credentials"},
				}},
				FailureDomains: []v1alpha1.VSphereFailureDomain{{
					Name:   "dc1-zone-a",
					Region: "dc1",
					Zone:   "a",
					Server: "vcenter.example.test",
					Topology: v1alpha1.VSphereFailureTopology{
						Datacenter:     "dc1",
						ComputeCluster: "/dc1/host/cluster1",
						Datastore:      "/dc1/datastore/datastore1",
						Networks:       []string{"lab-portgroup"},
					},
				}},
				MachineProfiles: []v1alpha1.MachineProfile{{Name: "node", CPU: 4, MemoryMiB: 16384, DiskGiB: 120}},
			},
		},
	}
	machines := make([]v1alpha1.Machine, 0, 3)
	hosts := make([]v1alpha1.OCPHostSpec, 0, 3)
	for _, name := range []string{"master-0", "master-1", "master-2"} {
		machines = append(machines, v1alpha1.Machine{
			Metadata: v1alpha1.Metadata{Name: name},
			Spec: v1alpha1.MachineSpec{
				OS: v1alpha1.MachineOSSpec{Provided: v1alpha1.BoolPtr(false)},
				Substrate: v1alpha1.MachineSubstrate{
					ProviderRef: v1alpha1.LocalObjectReference{Name: "lab-vsphere"},
					ProfileRef:  v1alpha1.LocalObjectReference{Name: "node"},
				},
			},
		})
		hosts = append(hosts, v1alpha1.OCPHostSpec{
			Hostname:   name,
			Role:       v1alpha1.NodeRoleMaster,
			MachineRef: v1alpha1.LocalObjectReference{Name: name},
		})
	}
	return v1alpha1.State{
		Environments: []v1alpha1.Environment{{
			Metadata: v1alpha1.Metadata{Name: "env"},
			Spec: v1alpha1.EnvironmentSpec{
				BaseDomain: "example.test",
				Secrets: map[string]v1alpha1.EnvironmentSecretSpec{
					"pull": {},
					"ssh": {
						Generated: &v1alpha1.EnvironmentSecretGenerated{
							SSHKeyPair: &v1alpha1.GeneratedSSHKeyPairSpec{Type: v1alpha1.SSHKeyPairTypeEd25519},
						},
					},
					"vcenter-credentials": {
						Generated: &v1alpha1.EnvironmentSecretGenerated{
							Credentials: &v1alpha1.GeneratedCredentialsSpec{Username: "administrator@vsphere.local"},
						},
					},
				},
			},
		}},
		InfraProviders: []v1alpha1.InfraProvider{provider},
		Machines:       machines,
		ContainerClusters: []v1alpha1.ContainerCluster{{
			Metadata: v1alpha1.Metadata{Name: "ocp"},
			Spec: v1alpha1.ContainerClusterSpec{
				Install: v1alpha1.OCPInstallSpec{
					Platform:      v1alpha1.InstallPlatform{Type: v1alpha1.PlatformTypeVSphere},
					PullSecretRef: v1alpha1.SecretRef{Name: "pull"},
					NodeSSH:       v1alpha1.NodeSSHSpec{KeyPairRef: v1alpha1.SecretRef{Name: "ssh"}},
				},
				Hosts: hosts,
			},
		}},
	}
}

// TestLoadInstallerSecretsResolvesVSphereCredentials covers the real-secrets
// install-config path for platform vsphere: the vCenter user/password
// material replaces the secret-ref placeholders, while the placeholder
// render keeps emitting placeholders only.
func TestLoadInstallerSecretsResolvesVSphereCredentials(t *testing.T) {
	secretsDir := t.TempDir()
	writeEncryptedSecret(t, secretsDir, "pull", secretstore.MaterialPrimary, `{"auths":{"quay.io":{"auth":"dXNlcjpwYXNz"}}}`)
	writeEncryptedSecret(t, secretsDir, "ssh", secretstore.MaterialSSHPublic, "ssh-ed25519 AAAA generated\n")
	writeEncryptedSecret(t, secretsDir, "vcenter-credentials", secretstore.MaterialPrimary, "administrator@vsphere.local:vc-password\n")
	state := vsphereInstallerTestState()
	ocp := state.ContainerClusters[0]

	secrets, err := LoadInstallerSecretsForContext("test", state, ocp, secretsDir)
	if err != nil {
		t.Fatalf("LoadInstallerSecrets: %v", err)
	}
	creds, ok := secrets.VSphereCredentials["vcenter-credentials"]
	if !ok {
		t.Fatalf("VSphereCredentials missing vcenter-credentials: %v", secrets.VSphereCredentials)
	}
	if creds.Username != "administrator@vsphere.local" || creds.Password != "vc-password" {
		t.Fatalf("vCenter credentials = %+v, want administrator@vsphere.local/vc-password", creds)
	}

	config, err := InstallerConfigWithSecrets(state, ocp, secrets)
	if err != nil {
		t.Fatalf("InstallerConfigWithSecrets: %v", err)
	}
	vc := installConfigFirstVCenter(t, config)
	if got := vc["user"]; got != "administrator@vsphere.local" {
		t.Fatalf("install-config vcenter user = %v, want resolved username", got)
	}
	if got := vc["password"]; got != "vc-password" {
		t.Fatalf("install-config vcenter password = %v, want resolved password", got)
	}
}

// TestPlaceholderInstallerConfigKeepsVSphereCredentialPlaceholders pins the
// placeholder render: without resolved material the vcenter user/password
// stay secret-ref placeholders and never leak bytes.
func TestPlaceholderInstallerConfigKeepsVSphereCredentialPlaceholders(t *testing.T) {
	state := vsphereInstallerTestState()
	config, err := InstallerConfig(state, state.ContainerClusters[0])
	if err != nil {
		t.Fatalf("InstallerConfig: %v", err)
	}
	vc := installConfigFirstVCenter(t, config)
	for key, marker := range map[string]string{
		"user":     "<bootwright-vsphere-user-ref:",
		"password": "<bootwright-vsphere-password-ref:",
	} {
		value, _ := vc[key].(string)
		if !strings.Contains(value, marker) {
			t.Fatalf("placeholder install-config vcenter %s = %q, want a %s placeholder", key, value, marker)
		}
	}
}

// TestPlatformConfigVSphereUserManagedLoadBalancer covers the external-LB
// case: when api/api-int/ingress are not all OpenShift-managed (e.g. a bastion
// haproxy owns the VIPs), the vSphere platform must declare
// loadBalancer.type=UserManaged so the integrated LB stays out of those VIPs.
func TestPlatformConfigVSphereUserManagedLoadBalancer(t *testing.T) {
	state := v1alpha1.State{}
	ocp := v1alpha1.ContainerCluster{Metadata: v1alpha1.Metadata{Name: "ocp"}}

	// No OpenShift-sourced endpoints -> userManaged -> loadBalancer UserManaged.
	externalLB := v1alpha1.ClusterInstall{}
	got := platformConfig(state, v1alpha1.PlatformTypeVSphere, externalLB, ocp, InstallerSecrets{})
	lb := vspherePlatformLoadBalancer(t, got)
	if lb == nil || lb["type"] != "UserManaged" {
		t.Fatalf("external-LB vsphere platform loadBalancer = %v, want type=UserManaged", lb)
	}

	// All three standard endpoints OpenShift-managed -> integrated LB (no field).
	openshiftManaged := v1alpha1.ClusterInstall{
		Endpoints: map[string]v1alpha1.Endpoint{
			v1alpha1.EndpointAPI:     {Source: v1alpha1.EndpointSource{Type: v1alpha1.EndpointSourceOpenShift}},
			v1alpha1.EndpointAPIInt:  {Source: v1alpha1.EndpointSource{Type: v1alpha1.EndpointSourceOpenShift}},
			v1alpha1.EndpointIngress: {Source: v1alpha1.EndpointSource{Type: v1alpha1.EndpointSourceOpenShift}},
		},
	}
	got = platformConfig(state, v1alpha1.PlatformTypeVSphere, openshiftManaged, ocp, InstallerSecrets{})
	if lb := vspherePlatformLoadBalancer(t, got); lb != nil {
		t.Fatalf("openshift-managed vsphere platform loadBalancer = %v, want none", lb)
	}
}

func vspherePlatformLoadBalancer(t *testing.T, platform map[string]any) map[string]any {
	t.Helper()
	vsphere, ok := platform["vsphere"].(map[string]any)
	if !ok {
		t.Fatalf("platform missing vsphere: %v", platform)
	}
	lb, _ := vsphere["loadBalancer"].(map[string]any)
	return lb
}

func installConfigFirstVCenter(t *testing.T, config map[string]any) map[string]any {
	t.Helper()
	platform, ok := config["platform"].(map[string]any)
	if !ok {
		t.Fatalf("install-config missing platform: %v", config)
	}
	vsphere, ok := platform["vsphere"].(map[string]any)
	if !ok {
		t.Fatalf("install-config platform missing vsphere: %v", platform)
	}
	vcenters, ok := vsphere["vcenters"].([]any)
	if !ok || len(vcenters) == 0 {
		t.Fatalf("install-config vsphere missing vcenters: %v", vsphere)
	}
	vc, ok := vcenters[0].(map[string]any)
	if !ok {
		t.Fatalf("install-config vcenters[0] is not a map: %v", vcenters[0])
	}
	return vc
}
