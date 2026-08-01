package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestReplaceArbiterPreviewNamesEveryGateTheRunConsults(t *testing.T) {
	auth, err := parseAuthorizations([]string{authorizeSameSiteArbiter}, authorizeVerbReplaceArbiter)
	if err != nil {
		t.Fatalf("parseAuthorizations: %v", err)
	}
	var out bytes.Buffer
	printRequiredAuthorizations(&out, replaceArbiterRequiredAuthorizations(auth, true, "mon.node-01 and mon.node-02 are outside quorum", "mon.node-08 shares datacenter=dc1 with mon.node-01"))
	text := out.String()
	for _, want := range []string{
		"Required authorizations",
		"--authorize " + authorizeSameSiteArbiter + ": " + authorizationSatisfied,
		"--authorize " + authorizeDegradedQuorum + ": " + authorizationRequired,
		"--authorize " + authorizeUnreachableNodes + ": " + authorizationMaybe,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("preview must contain %q; got:\n%s", want, text)
		}
	}
}

func TestReplaceArbiterPreviewOmitsAGateTheRunDoesNotReach(t *testing.T) {
	auth, err := parseAuthorizations(nil, authorizeVerbReplaceArbiter)
	if err != nil {
		t.Fatalf("parseAuthorizations: %v", err)
	}
	var out bytes.Buffer
	printRequiredAuthorizations(&out, replaceArbiterRequiredAuthorizations(auth, false, "", ""))
	text := out.String()
	for _, deny := range []string{authorizeSameSiteArbiter, authorizeDegradedQuorum} {
		if strings.Contains(text, "--authorize "+deny) {
			t.Errorf("preview must not name %q for a gate this run does not reach; got:\n%s", deny, text)
		}
	}
	if !strings.Contains(text, "--authorize "+authorizeUnreachableNodes+": "+authorizationMaybe) {
		t.Errorf("a host-decided gate stays disclosed; got:\n%s", text)
	}
}

func TestBlanketTokenSatisfiesEveryForecastEntry(t *testing.T) {
	auth, err := parseAuthorizations([]string{authorizeAll}, authorizeVerbReplaceArbiter)
	if err != nil {
		t.Fatalf("parseAuthorizations: %v", err)
	}
	for _, entry := range replaceArbiterRequiredAuthorizations(auth, true, "quorum is degraded", "same site") {
		if entry.Status != authorizationSatisfied {
			t.Errorf("--authorize %s must read %q under the blanket token, got %q", entry.Token, authorizationSatisfied, entry.Status)
		}
	}
}

func TestEmptyForecastStillEmitsTheBlock(t *testing.T) {
	var out bytes.Buffer
	printRequiredAuthorizations(&out, nil)
	if !strings.Contains(out.String(), "Required authorizations") {
		t.Errorf("a preview that reaches no gate still emits the block; got:\n%s", out.String())
	}
}
