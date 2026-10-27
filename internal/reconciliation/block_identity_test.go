package reconciliation

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

func TestABlockIdentityWithinItsBoundKeepsItsBytes(t *testing.T) {
	for _, tail := range []string{"a", strings.Repeat("a", 52), "lab-" + strings.Repeat("x", 48)} {
		if got := BlockIdentity("os-install-", tail); got != "os-install-"+tail {
			t.Fatalf("the identity os-install-%s moved to %q", tail, got)
		}
	}
	if got := BlockIdentity("", strings.Repeat("a", maxIdentifier)); got != strings.Repeat("a", maxIdentifier) {
		t.Fatalf("a 63-byte identity moved to %q", got)
	}
}

func TestALongTailHashesIntoABoundedDistinctIdentity(t *testing.T) {
	head := strings.Repeat("a", 60)
	first, second := BlockIdentity("os-install-", head+"-one"), BlockIdentity("os-install-", head+"-two")
	if first == second {
		t.Fatalf("two tails sharing a 60-byte head share the identity %q", first)
	}
	for _, id := range []string{first, second} {
		if len(id) != maxIdentifier || !ValidSegment(id) || !strings.HasPrefix(id, "os-install-"+strings.Repeat("a", 35)+"-") {
			t.Fatalf("the hashed identity %q is not the prefix, the fitting head and a 16-digit token", id)
		}
	}
	if again := BlockIdentity("os-install-", head+"-one"); again != first {
		t.Fatalf("the hashed identity is not stable: %q then %q", first, again)
	}
	if id := BlockIdentity("os-install-", strings.Repeat("a", 34)+"-"+strings.Repeat("b", 30)); strings.Contains(id, "--") || !ValidSegment(id) {
		t.Fatalf("a head ending at a hyphen kept it: %q", id)
	}
}

func TestEveryPrefixWithinTheCapLeavesRoomForTheToken(t *testing.T) {
	for size := 1; size <= MaxBlockPrefix; size++ {
		prefix := strings.Repeat("p", size)
		if id := BlockIdentity(prefix, strings.Repeat("t", maxIdentifier)); !ValidSegment(id) || !strings.HasPrefix(id, prefix) {
			t.Fatalf("a %d-byte prefix yields %q", size, id)
		}
	}
	if plan, err := NewPlan(Apply, []BlockDefinition{definition(BlockIdentity("ceph-service-", strings.Repeat("c", 63)+"-crash"))}); err != nil || len(plan.Blocks) != 1 {
		t.Fatalf("a hashed identity does not plan: %v", err)
	}
}

func TestAnyPrefixSplitOfTheSameBytesNamesTheSameBlock(t *testing.T) {
	name := strings.Repeat("c", 63)
	for _, short := range []string{"c", strings.Repeat("c", 49)} {
		if BlockIdentity("ceph-", "cluster-"+short) != BlockIdentity("ceph-cluster-", short) {
			t.Fatalf("a %d-byte name splits into two identities", len(short))
		}
	}
	generic, named := BlockIdentity("ceph-", "cluster-"+name), BlockIdentity("ceph-cluster-", name)
	if generic != named || len(named) != maxIdentifier || !strings.HasPrefix(named, "ceph-cluster-") {
		t.Fatalf("the split ceph-|cluster-%s names %q, the split ceph-cluster-|%s names %q", name, generic, name, named)
	}
}

func TestAShortNameThatSpellsALongNamesIdentityIsRefusedNamingBoth(t *testing.T) {
	long := strings.Repeat("c", 63)
	id := BlockIdentity("ceph-cluster-", long)
	short := strings.TrimPrefix(id, "ceph-cluster-")
	if BlockIdentity("ceph-cluster-", short) != id {
		t.Fatalf("the short name %q does not spell %q", short, id)
	}
	first, second := definition(id), definition(id)
	first.Kind, first.Object, second.Kind, second.Object = "CephCluster", long, "CephCluster", short
	_, err := NewPlan(Apply, []BlockDefinition{first, second})
	d := diagnostics.Of(err)
	if len(d) != 1 || d[0].Message != "CephCluster "+long+" and CephCluster "+short+" both name the block "+id || d[0].Remediation != "rename one of the two objects so their block identities differ" {
		t.Fatalf("the collision refusal is %+v", d)
	}
}
