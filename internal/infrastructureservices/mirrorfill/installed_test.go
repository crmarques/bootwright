package mirrorfill

import (
	"context"
	"encoding/json"
	"slices"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	machineref "github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
	secretmaterial "github.com/crmarques/bootwright/internal/secrets/material"
	"github.com/crmarques/bootwright/internal/trust"
)

const installedHostKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

// installedMirrorCatalog is mirrorCatalog with its mirror host a Machine
// Bootwright installs, reached as its installation configured it.
func installedMirrorCatalog() api.Catalog {
	return installedHost(mirrorCatalog("mirror"))
}

func installedHost(catalog api.Catalog) api.Catalog {
	objects := catalog.Objects()
	for index, object := range objects {
		if object.Kind() == api.Machine && object.Name() == "mirror" {
			ssh := api.MapValue(text("addressRef", "ip"), field("port", api.IntegerValue("22")), text("user", "bootwright"), field("auth", api.MapValue(text("privateKeyRef", "fleet-key"))))
			network := api.MapValue(text("installAddressRef", "ip"), field("addresses", api.ListValue(api.MapValue(text("name", "ip"), text("address", "203.0.113.20")))))
			spec := object.Spec().With("os", api.MapValue(field("provided", api.BoolValue(false)), text("installProfileRef", "rhel")))
			objects[index] = object.WithSpec(spec.With("network", network).With("access", api.MapValue(field("ssh", ssh))))
		}
	}
	return api.NewCatalog(objects)
}

// The fill on a mirror host Bootwright installs waits for that installation as
// well as the server, binds no host-key Secret, and connects against the host
// key the installation proved, which composition's reader lends it.
func TestAFillOnAnInstalledMirrorHostConnectsAgainstItsInstallationsHostKey(t *testing.T) {
	plan := planned(t, installedMirrorCatalog(), clientArtifacts)
	if len(plan.Definitions) != 1 || !slices.Contains(plan.Definitions[0].Requires, reconciliation.ObjectRef{Kind: string(api.Machine), Object: "mirror", Family: machineref.InstallFamily}) ||
		!slices.Equal(plan.Secrets, []string{"fleet-key"}) {
		t.Fatalf("plan = %+v", plan)
	}
	digest, err := reconciliation.RequestDigest(plan.Definitions[0])
	if err != nil {
		t.Fatal(err)
	}
	block := reconciliation.Block{BlockDefinition: plan.Definitions[0], RequestDigest: digest}
	request, _ := DecodeFilesRequest(block.Request)
	if !request.Placement.Installed() || request.Placement.KnownHostsRef != "" || request.Placement.User != "bootwright" {
		t.Fatalf("placement = %+v", request.Placement)
	}
	complete, _ := json.Marshal(FilesEvidence{Files: []FileEvidence{{Path: request.Files[0].Path, Verified: true}, {Path: request.Files[1].Path, Verified: true}},
		Postcondition: true, Request: digest, Version: FilesEvidenceVersion})
	runner := &scriptedRunner{result: lifecycle.RunResult{Outcome: "unchanged", Evidence: complete}}
	proved := func(_ []lifecycle.BlockEvidence, machine, address string) (trust.HostKey, error) {
		return trust.ParseAuthorizedKey(installedHostKey)
	}
	execution := lifecycle.Execution{Context: "lab", Block: block}
	if _, err := NewFiles(runner, clientArtifacts).Apply(context.Background(), execution); err == nil {
		t.Fatal("a fill with no installation proof reader ran")
	}
	if result, err := NewFiles(runner, clientArtifacts).WithInstalledHostKeys(proved).Apply(context.Background(), execution); err != nil || result.Outcome != reconciliation.OutcomeUnchanged {
		t.Fatalf("apply = %+v (%v)", result, err)
	}
	line, _ := runner.request.Material[lifecycle.InstalledHostKeyMaterial].Part(secrets.ValuePart)
	if runner.request.Placement.KnownHostsRef != lifecycle.InstalledHostKeyMaterial || string(line) != "203.0.113.20 "+installedHostKey+"\n" {
		t.Fatalf("the run was placed %+v with %q", runner.request.Placement, line)
	}
}

// The image fill on a mirror host Bootwright installs, like the file fill,
// waits for that installation beside the Registry, binds no host-key Secret
// and connects against the host key the installation proved.
func TestAnImageFillOnAnInstalledMirrorHostConnectsAgainstItsInstallationsHostKey(t *testing.T) {
	plan := plannedImages(t, installedHost(registryCatalog()), pinnedImages)
	if len(plan.Definitions) != 1 || !slices.Equal(plan.Definitions[0].Requires, []reconciliation.ObjectRef{
		{Kind: "Registry", Object: "mirror-registry", Family: "managed-service"}, {Kind: string(api.Machine), Object: "mirror", Family: machineref.InstallFamily}}) ||
		!slices.Equal(plan.Secrets, []string{"fleet-key", "mirror-ca", "mirror-registry-credentials"}) {
		t.Fatalf("plan = %+v", plan)
	}
	digest, err := reconciliation.RequestDigest(plan.Definitions[0])
	if err != nil {
		t.Fatal(err)
	}
	block := reconciliation.Block{BlockDefinition: plan.Definitions[0], RequestDigest: digest}
	request, _ := DecodeImagesRequest(block.Request)
	if !request.Placement.Installed() || request.Placement.KnownHostsRef != "" || request.Placement.User != "bootwright" {
		t.Fatalf("placement = %+v", request.Placement)
	}
	var images []ImageEvidence
	for _, image := range request.Images {
		images = append(images, ImageEvidence{Repository: image.Repository, Digest: image.Digest, Verified: true})
	}
	complete, _ := json.Marshal(ImagesEvidence{Images: images, Postcondition: true, Request: digest, Version: ImagesEvidenceVersion})
	runner := &scriptedRunner{result: lifecycle.RunResult{Outcome: "unchanged", Evidence: complete}}
	proved := func(_ []lifecycle.BlockEvidence, machine, address string) (trust.HostKey, error) {
		return trust.ParseAuthorizedKey(installedHostKey)
	}
	execution := lifecycle.Execution{Context: "lab", Block: block}
	if _, err := NewImages(runner, pinnedImages).Apply(context.Background(), execution); err == nil {
		t.Fatal("an image fill with no installation proof reader ran")
	}
	if result, err := NewImages(runner, pinnedImages).WithInstalledHostKeys(proved).Apply(context.Background(), execution); err != nil || result.Outcome != reconciliation.OutcomeUnchanged {
		t.Fatalf("apply = %+v (%v)", result, err)
	}
	line, _ := runner.request.Material[lifecycle.InstalledHostKeyMaterial].Part(secrets.ValuePart)
	if runner.request.Placement.KnownHostsRef != lifecycle.InstalledHostKeyMaterial || string(line) != "203.0.113.20 "+installedHostKey+"\n" {
		t.Fatalf("the run was placed %+v with %q", runner.request.Placement, line)
	}
}

// A release copy on a mirror host Bootwright installs waits for that
// installation beside the Registry and connects against the host key the
// installation proved, still lent its two-entry auth document.
func TestAReleaseCopyOnAnInstalledMirrorHostConnectsAgainstItsInstallationsHostKey(t *testing.T) {
	plan := plannedReleases(t, installedHost(registryCatalog()), clusterReleases(releaseDigest))
	if len(plan.Definitions) != 2 {
		t.Fatalf("plan = %+v", plan)
	}
	for _, definition := range plan.Definitions {
		if !slices.Equal(definition.Requires, []reconciliation.ObjectRef{
			{Kind: "Registry", Object: "mirror-registry", Family: "managed-service"}, {Kind: string(api.Machine), Object: "mirror", Family: machineref.InstallFamily}}) {
			t.Fatalf("%s requires %v", definition.ID, definition.Requires)
		}
	}
	if !slices.Contains(plan.Secrets, "fleet-key") || slices.Contains(plan.Secrets, "mirror-known-hosts") {
		t.Fatalf("secrets = %v", plan.Secrets)
	}
	definition := plan.Definitions[1]
	digest, err := reconciliation.RequestDigest(definition)
	if err != nil {
		t.Fatal(err)
	}
	block := reconciliation.Block{BlockDefinition: definition, RequestDigest: digest}
	request, _ := DecodeReleaseRequest(block.Request)
	if !request.Placement.Installed() || request.Placement.KnownHostsRef != "" || request.Placement.User != "bootwright" {
		t.Fatalf("placement = %+v", request.Placement)
	}
	complete, _ := json.Marshal(ReleaseEvidence{Components: []ComponentEvidence{{Name: "cli", Digest: registryDigest}}, Digest: releaseDigest,
		Postcondition: true, Request: digest, Version: ReleaseEvidenceVersion})
	runner := &lendingRunner{}
	runner.result = lifecycle.RunResult{Outcome: "changed", Evidence: complete}
	proved := func(_ []lifecycle.BlockEvidence, machine, address string) (trust.HostKey, error) {
		return trust.ParseAuthorizedKey(installedHostKey)
	}
	execution := lifecycle.Execution{Context: "lab", Block: block, Material: releaseMaterial()}
	if _, err := NewReleases(runner, clusterReleases(releaseDigest), secretmaterial.RegistryAuth).Apply(context.Background(), execution); err == nil {
		t.Fatal("a release copy with no installation proof reader ran")
	}
	execution.Material = releaseMaterial()
	capability := NewReleases(runner, clusterReleases(releaseDigest), secretmaterial.RegistryAuth).WithInstalledHostKeys(proved)
	if result, err := capability.Apply(context.Background(), execution); err != nil || result.Outcome != reconciliation.OutcomeChanged {
		t.Fatalf("apply = %+v (%v)", result, diagnostics.Of(err))
	}
	line, _ := runner.request.Material[lifecycle.InstalledHostKeyMaterial].Part(secrets.ValuePart)
	if runner.request.Placement.KnownHostsRef != lifecycle.InstalledHostKeyMaterial || string(line) != "203.0.113.20 "+installedHostKey+"\n" || len(runner.document) == 0 {
		t.Fatalf("the run was placed %+v with %q and lent %d document bytes", runner.request.Placement, line, len(runner.document))
	}
}
