package cli

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/converge"
)

func TestStageFlagCompletionOffersCanonicalValues(t *testing.T) {
	applyOut, _, code := runCLI(t, "__complete", "apply", "--stage", "")
	if code != 0 {
		t.Fatalf("apply __complete exit = %d, out=%q", code, applyOut)
	}
	for _, want := range converge.ApplyStageNames() {
		if !strings.Contains(applyOut, want) {
			t.Fatalf("apply --stage completion missing %q:\n%s", want, applyOut)
		}
	}

	destroyOut, _, code := runCLI(t, "__complete", "destroy", "--stage", "")
	if code != 0 {
		t.Fatalf("destroy __complete exit = %d, out=%q", code, destroyOut)
	}
	for _, want := range converge.DestroyStageNames() {
		if !strings.Contains(destroyOut, want) {
			t.Fatalf("destroy --stage completion missing %q:\n%s", want, destroyOut)
		}
	}
	if strings.Contains(destroyOut, "fabric") {
		t.Fatalf("destroy --stage completion offered an apply-only sub-phase:\n%s", destroyOut)
	}
}

func TestProviderAndPartFlagCompletionOfferEnumValues(t *testing.T) {
	providerOut, _, code := runCLI(t, "__complete", "example", "init", "--provider", "")
	if code != 0 {
		t.Fatalf("example init --provider __complete exit = %d, out=%q", code, providerOut)
	}
	if !strings.Contains(providerOut, "bare-metal") || !strings.Contains(providerOut, "kubevirt") {
		t.Fatalf("--provider completion missing known providers:\n%s", providerOut)
	}

	partOut, _, code := runCLI(t, "__complete", "secret", "show", "--part", "")
	if code != 0 {
		t.Fatalf("secret show --part __complete exit = %d, out=%q", code, partOut)
	}
	for _, want := range []string{"primary", "private", "public", "tls-key"} {
		if !strings.Contains(partOut, want) {
			t.Fatalf("--part completion missing %q:\n%s", want, partOut)
		}
	}
}

func TestClusterAndMachineFlagCompletionFromStorage(t *testing.T) {
	setTestHomeAndRoot(t)
	inputDir := copyFixtureYAML(t, "001-sno-libvirt")
	if stdout, stderr, code := runCLI(t, "context", "init", "--name", "test", "-f", inputDir); code != 0 {
		t.Fatalf("context init exited %d, stdout=%q stderr=%q", code, stdout, stderr)
	}

	clustersOut, _, code := runCLI(t, "__complete", "apply", "--clusters", "")
	if code != 0 {
		t.Fatalf("apply --clusters __complete exit = %d, out=%q", code, clustersOut)
	}
	if !strings.Contains(clustersOut, "sno-libvirt") {
		t.Fatalf("apply --clusters completion missing declared cluster:\n%s", clustersOut)
	}

	machinesOut, _, code := runCLI(t, "__complete", "machine", "trust", "--machines", "")
	if code != 0 {
		t.Fatalf("machine trust --machines __complete exit = %d, out=%q", code, machinesOut)
	}
	for _, want := range []string{"master-0", "bastion"} {
		if !strings.Contains(machinesOut, want) {
			t.Fatalf("machine trust --machines completion missing %q:\n%s", want, machinesOut)
		}
	}

	dedupOut, _, code := runCLI(t, "__complete", "machine", "trust", "--machines", "master-0,")
	if code != 0 {
		t.Fatalf("machine trust --machines dedup __complete exit = %d, out=%q", code, dedupOut)
	}
	if !strings.Contains(dedupOut, "master-0,bastion") {
		t.Fatalf("comma-separated completion should append to the prior selection:\n%s", dedupOut)
	}
	if strings.Contains(dedupOut, "master-0,master-0") {
		t.Fatalf("comma-separated completion re-offered an already-selected machine:\n%s", dedupOut)
	}

	for _, cmd := range [][]string{
		{"__complete", "apply", "--machines", ""},
		{"__complete", "destroy", "--machines", ""},
	} {
		out, _, code := runCLI(t, cmd...)
		if code != 0 {
			t.Fatalf("%v __complete exit = %d, out=%q", cmd, code, out)
		}
		for _, want := range []string{"master-0", "bastion"} {
			if !strings.Contains(out, want) {
				t.Fatalf("%v completion missing %q:\n%s", cmd, want, out)
			}
		}
	}

	ctxOut, _, code := runCLI(t, "__complete", "context", "use", "--name", "")
	if code != 0 {
		t.Fatalf("context use --name __complete exit = %d, out=%q", code, ctxOut)
	}
	if !strings.Contains(ctxOut, "test") {
		t.Fatalf("context use --name completion missing declared context:\n%s", ctxOut)
	}
}
