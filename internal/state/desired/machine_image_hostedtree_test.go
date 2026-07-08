package desiredstate

import (
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func hostedTreeSource() *v1alpha1.MachineInstallPackageSource {
	return &v1alpha1.MachineInstallPackageSource{
		HostedTree: &v1alpha1.MachineInstallPackageHostedTree{
			FromMedia: "local-media:rhel-9.7-x86_64-dvd.iso",
			ArtifactServerEndpoint: v1alpha1.ArtifactServerEndpointRef{
				EndpointRef: v1alpha1.LocalObjectReference{Name: "tree"},
			},
		},
	}
}

func TestMachineInstallHostedTreeAcceptsFromMedia(t *testing.T) {
	errs := validateMachineInstallProfiles(machineInstallPackageSourceState(hostedTreeSource()))
	if len(errs) != 0 {
		t.Fatalf("validateMachineInstallProfiles errors = %v", errs)
	}
}

func TestMachineInstallHostedTreeRequiresFromMedia(t *testing.T) {
	source := hostedTreeSource()
	source.HostedTree.FromMedia = ""
	errs := validateMachineInstallProfiles(machineInstallPackageSourceState(source))
	if !containsSubstring(errs, "packageSource.hostedTree.fromMedia is required") {
		t.Fatalf("errors = %v, want fromMedia requirement", errs)
	}
}

func TestMachineInstallHostedTreeRequiresArtifactServerEndpoint(t *testing.T) {
	source := hostedTreeSource()
	source.HostedTree.ArtifactServerEndpoint = v1alpha1.ArtifactServerEndpointRef{}
	errs := validateMachineInstallProfiles(machineInstallPackageSourceState(source))
	if !containsSubstring(errs, "packageSource.hostedTree.artifactServerEndpoint.endpointRef is required") {
		t.Fatalf("errors = %v, want artifactServerEndpoint requirement", errs)
	}
}

func TestMachineInstallHostedTreeRejectsFromMediaEqualToBootMedia(t *testing.T) {
	source := hostedTreeSource()
	state := machineInstallPackageSourceState(source)
	source.HostedTree.FromMedia = state.MachineImages[0].Spec.BootMedia
	errs := validateMachineInstallProfiles(state)
	if !containsSubstring(errs, "must reference the DVD, not the boot ISO") {
		t.Fatalf("errors = %v, want fromMedia!=bootMedia requirement", errs)
	}
}

func TestMachineInstallHostedTreeRejectsURLFromMedia(t *testing.T) {
	source := hostedTreeSource()
	source.HostedTree.FromMedia = "https://mirror.example.test/rhel/9/rhel-9.7-dvd.iso"
	errs := validateMachineInstallProfiles(machineInstallPackageSourceState(source))
	if !containsSubstring(errs, "fromMedia must reference local media") {
		t.Fatalf("errors = %v, want local-media requirement", errs)
	}
}

func TestMachineInstallHostedTreeRejectsExternalArtifactServer(t *testing.T) {
	source := hostedTreeSource()
	state := machineInstallPackageSourceState(source)
	state.Environments[0].Spec.InfraComponents.ArtifactServers[0] = v1alpha1.EnvironmentArtifactServerComponent{
		Name:       "default",
		Management: v1alpha1.EnvironmentComponentExternal,
		Endpoints: []v1alpha1.EnvironmentArtifactServerEndpoint{{
			Name: "tree",
			URL:  "http://artifacts.example.test/rhel/",
		}},
	}
	errs := validateMachineInstallProfiles(state)
	if !containsSubstring(errs, `packageSource.hostedTree.artifactServerEndpoint.serverRef "default" must select a managed artifact server`) {
		t.Fatalf("errors = %v, want managed artifact server requirement", errs)
	}
}

func TestMachineInstallHostedTreeRejectsHTTPSArtifactServerEndpoint(t *testing.T) {
	source := hostedTreeSource()
	state := machineInstallPackageSourceState(source)
	state.InfraComponents[0].Spec.ArtifactServer.Listeners[0].Protocol = v1alpha1.ArtifactServerProtocolHTTPS
	errs := validateMachineInstallProfiles(state)
	if !containsSubstring(errs, `packageSource.hostedTree.artifactServerEndpoint.endpointRef "tree" must select an http artifact server endpoint`) {
		t.Fatalf("errors = %v, want http artifact endpoint requirement", errs)
	}
}
