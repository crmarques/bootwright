package mirrorfill

import (
	"context"
	"encoding/json"
	"slices"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	machineref "github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/trust"
)

const installedHostKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

// installedMirrorCatalog is mirrorCatalog with its mirror host a Machine
// Bootwright installs, reached as its installation configured it.
func installedMirrorCatalog() api.Catalog {
	objects := mirrorCatalog("mirror").Objects()
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
