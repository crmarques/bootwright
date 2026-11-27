package clients

import (
	"bytes"
	"context"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/secrets"
)

func trustedMirror(extra ...api.FieldValue) api.FieldValue {
	return field("mirror", api.MapValue(field("artifacts", api.MapValue(append([]api.FieldValue{text("url", "https://mirror.example.test/tools")}, extra...)...))))
}

// Only the anchor Secret's name is frozen, and only when a client of the
// request is read from the mirror; the block names it as the one Secret its
// attempt is lent.
func TestTheRequestFreezesTheMirrorAnchorByNameAlone(t *testing.T) {
	block := planBlock(t, unwired(), stateOf(environment(trustedMirror(text("trustBundleRef", "mirror-ca"))), machine(), cluster()), reconciliation.Apply)
	request, err := DecodeRequest(block.Request)
	if err != nil {
		t.Fatal(err)
	}
	if request.MirrorTrust == nil || *request.MirrorTrust != (MirrorTrust{Host: "mirror.example.test", TrustBundleRef: "mirror-ca"}) {
		t.Fatalf("mirror trust = %+v", request.MirrorTrust)
	}
	references, err := unwired().SecretReferences(block)
	if err != nil || !slices.Equal(references, []string{"mirror-ca"}) {
		t.Fatalf("references = %v (%v)", references, err)
	}
	for _, objects := range [][]api.Object{
		{environment(trustedMirror()), machine(), cluster()},
		{environment(), machine(), cluster()},
	} {
		block := planBlock(t, unwired(), stateOf(objects...), reconciliation.Apply)
		if bytes.Contains(block.Request, []byte("mirrorTrust")) {
			t.Fatalf("a request without an anchor froze one: %s", block.Request)
		}
		if references, err := unwired().SecretReferences(block); err != nil || len(references) != 0 {
			t.Fatalf("a request without an anchor names %v (%v)", references, err)
		}
	}
}

// The engine uses the contribution to bind Secrets and the block reference to
// scope their use. A trust reference in the frozen request alone lends nothing.
func TestTheControllerPlanBindsEachMirrorAnchorItUses(t *testing.T) {
	for _, tc := range []struct {
		name    string
		objects []api.Object
		refs    []string
		blocks  int
		reader  string
	}{
		{"managed mirror", controllerMirrorState(), []string{"mirror-ca"}, 2, MirroredClientsBlockID},
		{"operator mirror", []api.Object{environment(trustedMirror(text("trustBundleRef", "mirror-ca"))), machine(), cluster()}, []string{"mirror-ca"}, 1, BlockID},
		{"mirror without an anchor", []api.Object{environment(trustedMirror()), machine(), cluster()}, nil, 1, ""},
		{"no mirror", []api.Object{environment(), machine(), cluster()}, nil, 1, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			capability := unwired()
			plan, err := capability.Plan(context.Background(), planInput(stateOf(tc.objects...), reconciliation.Apply))
			if err != nil || len(plan.Definitions) != tc.blocks {
				t.Fatalf("planned %d blocks (%v), want %d", len(plan.Definitions), err, tc.blocks)
			}
			if !slices.Equal(plan.Secrets, tc.refs) {
				t.Fatalf("bound Secrets = %v, want %v", plan.Secrets, tc.refs)
			}
			for _, definition := range plan.Definitions {
				refs, err := capability.SecretReferences(reconciliation.Block{BlockDefinition: definition})
				var want []string
				if definition.ID == tc.reader {
					want = tc.refs
				}
				if err != nil || !slices.Equal(refs, want) {
					t.Fatalf("block %s references %v (%v), want %v", definition.ID, refs, err, want)
				}
			}
		})
	}
}

// The attempt hands the run the lent certificate for the mirror's host alone,
// and refuses before any effect when it was lent none.
func TestTheStageLendsTheMirrorAnchorToTheRunThatDownloads(t *testing.T) {
	state := stateOf(environment(trustedMirror(text("trustBundleRef", "mirror-ca"))), machine(), cluster())
	anchor := []byte("-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\n")
	resolved := []prerequisites.ToolDefinition{toolDefinition("helm", "v3.17.0")}
	installer := &fakeInstaller{}
	capability := stage(&fakeTools{resolved: resolved, present: true}, newDnf(), &fakeSetup{}, installer)
	block := planBlock(t, capability, state, reconciliation.Apply)
	recorder := newRecorder(&fakeArea{})
	execution := recorder.execution(t, block, hostState())
	execution.Material = map[string]secrets.Material{"mirror-ca": secrets.NewMaterial(map[secrets.Part][]byte{secrets.CertificatePart: anchor})}
	if result, err := capability.Apply(context.Background(), execution); err != nil || result.Outcome != reconciliation.OutcomeChanged {
		t.Fatalf("result = %+v (%v)", result, err)
	}
	if installer.publication.Trust.Host != "mirror.example.test" || !bytes.Equal(installer.publication.Trust.Anchor, anchor) {
		t.Fatalf("the run was lent %+v", installer.publication.Trust)
	}
	installer = &fakeInstaller{}
	capability = stage(&fakeTools{resolved: resolved, present: true}, newDnf(), &fakeSetup{}, installer)
	recorder = newRecorder(&fakeArea{})
	execution = recorder.execution(t, block, hostState())
	execution.Context = "lab"
	result, err := capability.Apply(context.Background(), execution)
	if firstCode(err) != "secret.part" || result.Outcome != reconciliation.OutcomeFailed || installer.installs != 0 || !strings.Contains(diagnostics.Of(err)[0].Message, "Secret mirror-ca") {
		t.Fatalf("an attempt lent no anchor = %+v (%v) after %d runs", result, err, installer.installs)
	}
	if remedy := diagnostics.Of(err)[0].Remediation; !strings.Contains(remedy, "bootwright destroy --dry-run --context lab") || strings.Contains(remedy, "secret set") {
		t.Fatalf("a frozen apply missing its anchor directs recovery to %q", remedy)
	}
}

func TestADecodedMirrorAnchorMustNameAHostItsClientsAreReadFrom(t *testing.T) {
	block := planBlock(t, unwired(), stateOf(environment(trustedMirror(text("trustBundleRef", "mirror-ca"))), machine(), cluster()), reconciliation.Apply)
	request, err := DecodeRequest(block.Request)
	if err != nil {
		t.Fatal(err)
	}
	request.MirrorTrust.Host = "other.example.test"
	data, err := request.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeRequest(data); firstCode(err) != "clients.request-unreadable" {
		t.Fatalf("decode = %v", err)
	}
}
