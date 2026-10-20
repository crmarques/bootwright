package ceph

import (
	"bytes"
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func withSpecializations(t *testing.T, data []byte, before, member string) []byte {
	t.Helper()
	anchor := `,"` + before + `":`
	if bytes.Count(data, []byte(anchor)) != 1 {
		t.Fatalf("no single %s member in %s", before, data)
	}
	return bytes.Replace(data, []byte(anchor), []byte(`,"specializations":`+member+anchor), 1)
}

func malformedSpecializations() map[string]string {
	ids := make([]string, 33)
	for i := range ids {
		ids[i] = `"id-` + string(rune('a'+i%26)) + strings.Repeat("a", i/26) + `"`
	}
	slices.Sort(ids)
	return map[string]string{
		"empty":     `[]`,
		"null":      `null`,
		"unsorted":  `["ceph-mgr-without-license-contract","ceph-bootstrap-license-call-continues"]`,
		"duplicate": `["ceph-mgr","ceph-mgr"]`,
		"bad id":    `["Ceph_X"]`,
		"long id":   `["` + strings.Repeat("a", 65) + `"]`,
		"33 ids":    `[` + strings.Join(ids, ",") + `]`,
	}
}

func TestEvidenceSpecializationsMemberIsOptionalAndClosed(t *testing.T) {
	r := runtimeRequest()
	plain := evidenceBytes(t, readyEvidence(r))
	if bytes.Contains(plain, []byte("specializations")) {
		t.Fatal("evidence without specializations names the member")
	}
	if effect, err := EvaluateEvidence(plain, r, "digest", false); err != nil || effect != reconciliation.EffectCompleted {
		t.Fatalf("a record without the member read as %q (%v)", effect, err)
	}
	applied := []string{"ceph-bootstrap-license-call-continues", "ceph-mgr-without-license-contract"}
	named := readyEvidence(r)
	named.Specializations = applied
	data := evidenceBytes(t, named)
	if !bytes.Contains(data, []byte(`"specializations":["ceph-bootstrap-license-call-continues","ceph-mgr-without-license-contract"]`)) {
		t.Fatalf("the member was not marshalled: %s", data)
	}
	if decoded, err := decodeEvidence(data); err != nil || !slices.Equal(decoded.Specializations, applied) {
		t.Fatalf("the member decoded as %v (%v)", decoded.Specializations, err)
	}
	if effect, err := EvaluateEvidence(data, r, "digest", false); err != nil || effect != reconciliation.EffectCompleted {
		t.Fatalf("a record with the member read as %q (%v)", effect, err)
	}
	for name, member := range malformedSpecializations() {
		_, err := decodeEvidence(withSpecializations(t, plain, "version", member))
		if d := diagnostics.Of(err); len(d) != 1 || d[0].Message != "Ceph evidence does not prove the frozen ownership and resource scope" {
			t.Errorf("%s: %v", name, d)
		}
	}
	if _, err := decodeEvidence(withSpecializations(t, plain, "version", `1`)); err == nil {
		t.Error("a number was accepted as the member")
	}
}

func TestHostEvidenceSpecializationsMemberIsOptionalAndClosed(t *testing.T) {
	e, r, proofs := hostExecution(t, "node-3")
	plain := preparedEvidence(t, e, r, nil)
	if bytes.Contains(plain, []byte("specializations")) {
		t.Fatal("host evidence without specializations names the member")
	}
	observe := func(evidence json.RawMessage) (lifecycle.Observation, error) {
		runner := &recordingRunner{result: lifecycle.RunResult{Outcome: "unchanged", Evidence: evidence}}
		return NewHost(runner).WithProofs(proofs).Observe(context.Background(), e)
	}
	named := preparedEvidence(t, e, r, func(v *HostEvidence) { v.Specializations = []string{"ceph-host-pull"} })
	for name, evidence := range map[string]json.RawMessage{"absent": plain, "one id": named} {
		if observation, err := observe(evidence); err != nil || observation.Effect != reconciliation.EffectCompleted {
			t.Fatalf("%s: observed %q (%v)", name, observation.Effect, diagnostics.Of(err))
		}
		proof := lifecycle.BlockEvidence{Kind: e.Block.Kind, Object: e.Block.Object, Implementation: HostImplementation, Verb: reconciliation.Apply, State: reconciliation.BlockDone, Evidence: evidence, Block: e.Block}
		if proved, err := ProvedHost([]lifecycle.BlockEvidence{proof}, "example", "node-3", PinnedRepository+"@"+resolvedDigest); err != nil || proved.Host != "node-3" {
			t.Fatalf("%s: proved %+v (%v)", name, proved, diagnostics.Of(err))
		}
	}
	for name, member := range malformedSpecializations() {
		_, err := observe(withSpecializations(t, plain, "uid", member))
		if d := diagnostics.Of(err); len(d) != 1 || d[0].Message != "Ceph host evidence names malformed specialization ids" {
			t.Errorf("%s: %v", name, d)
		}
	}
	earlier := preparedEvidence(t, e, r, func(v *HostEvidence) { v.Version = "ceph-host-evidence-v1" })
	if _, err := observe(earlier); len(diagnostics.Of(err)) != 1 || diagnostics.Of(err)[0].Message != "Ceph host evidence names another request" {
		t.Fatalf("an earlier host evidence version read as %v", diagnostics.Of(err))
	}
}
