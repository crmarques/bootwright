package sshlocal

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

const policyCorpus = "../../../ansible/collections/ansible_collections/bootwright/core/tests/unit/goldens/ssh_crypto_policy_lines.json"

type policyCase struct {
	Case    string          `json:"case"`
	Policy  json.RawMessage `json:"policy"`
	Verdict string          `json:"verdict"`
	Config  *string         `json:"config"`
}

func (c policyCase) text(t *testing.T) string {
	t.Helper()
	var plain string
	if json.Unmarshal(c.Policy, &plain) == nil {
		return plain
	}
	var padded struct {
		Head, Fill, Tail string
		Total            int
	}
	if err := json.Unmarshal(c.Policy, &padded); err != nil {
		t.Fatal(err)
	}
	return padded.Head + strings.Repeat(padded.Fill, padded.Total-len(padded.Head)-len(padded.Tail)) + padded.Tail
}

// The reducer a session reads the host policy through gives the verdict and
// the configuration the shared corpus names, the same ones the two Python
// reducers give, including a refusal of every directive that is neither
// cryptographic selection nor allowlisted.
func TestThePolicyReducerFollowsTheSharedCorpus(t *testing.T) {
	data, err := os.ReadFile(policyCorpus)
	if err != nil {
		t.Fatal(err)
	}
	var corpus struct {
		Cases []policyCase `json:"cases"`
	}
	if err := json.Unmarshal(data, &corpus); err != nil {
		t.Fatal(err)
	}
	if len(corpus.Cases) == 0 {
		t.Fatal("the policy corpus holds no case")
	}
	for _, c := range corpus.Cases {
		t.Run(c.Case, func(t *testing.T) {
			policy := c.text(t)
			got, err := cryptographicPolicy([]byte(policy), true)
			if c.Verdict == "refuse" {
				if err == nil {
					t.Fatalf("the policy was accepted as %q", got)
				}
				if reported := diagnostics.Of(err); len(reported) == 0 || reported[0].Code != "access.unavailable" {
					t.Fatalf("diagnostic = %+v", reported)
				}
				return
			}
			want := policy
			if c.Config != nil {
				want = *c.Config
			}
			if err != nil || string(got) != want {
				t.Fatalf("configuration %q (%v), want %q", got, err, want)
			}
		})
	}
}
