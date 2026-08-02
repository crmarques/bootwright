package cli

import (
	"bytes"
	"strings"
	"testing"
)

const (
	arbiterDegradedReason = "mon(s) node-05 are outside quorum"
	arbiterSameSiteDetail = "mon.node-07 sits at datacenter=dc3, which already holds non-tiebreaker mon(s) node-08"
)

func TestReplaceArbiterPreviewNamesBothGatesAsRequiredWithoutAnyToken(t *testing.T) {
	auth, err := parseAuthorizations(nil, authorizeVerbReplaceArbiter)
	if err != nil {
		t.Fatalf("parseAuthorizations: %v", err)
	}
	var out bytes.Buffer
	printRequiredAuthorizations(&out, replaceArbiterRequiredAuthorizations(auth, true, arbiterDegradedReason, arbiterSameSiteDetail))
	text := out.String()
	for _, want := range []string{
		"Required authorizations",
		"--authorize " + authorizeSameSiteArbiter + ": " + authorizationRequired,
		"--authorize " + authorizeDegradedQuorum + ": " + authorizationRequired,
		"--authorize " + authorizeUnreachableNodes + ": " + authorizationMaybe,
		arbiterSameSiteDetail,
		arbiterDegradedReason,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("one preview must contain %q; got:\n%s", want, text)
		}
	}
	if got := strings.Count(text, "Required authorizations"); got != 1 {
		t.Errorf("the whole blast radius must be learnable from one block, got %d blocks:\n%s", got, text)
	}
}

func TestReplaceArbiterPreviewReadsASuppliedTokenAsSatisfied(t *testing.T) {
	auth, err := parseAuthorizations([]string{authorizeDegradedQuorum}, authorizeVerbReplaceArbiter)
	if err != nil {
		t.Fatalf("parseAuthorizations: %v", err)
	}
	var out bytes.Buffer
	printRequiredAuthorizations(&out, replaceArbiterRequiredAuthorizations(auth, true, arbiterDegradedReason, arbiterSameSiteDetail))
	text := out.String()
	for _, want := range []string{
		"--authorize " + authorizeDegradedQuorum + ": " + authorizationSatisfied,
		"--authorize " + authorizeSameSiteArbiter + ": " + authorizationRequired,
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

func TestForecastingAGateNeverConsumesTheTokenThatClearsIt(t *testing.T) {
	auth, err := parseAuthorizations([]string{authorizeSameSiteArbiter, authorizeDegradedQuorum}, authorizeVerbReplaceArbiter)
	if err != nil {
		t.Fatalf("parseAuthorizations: %v", err)
	}
	replaceArbiterRequiredAuthorizations(auth, true, arbiterDegradedReason, arbiterSameSiteDetail)
	unused := auth.unused()
	for _, name := range []string{authorizeSameSiteArbiter, authorizeDegradedQuorum} {
		if !strings.Contains(strings.Join(unused, ","), name) {
			t.Errorf("a preview reports gates without consulting them, so --authorize %s must stay unconsumed; unused=%v", name, unused)
		}
	}
}

func TestBlanketTokenSatisfiesEveryForecastEntry(t *testing.T) {
	auth, err := parseAuthorizations([]string{authorizeAll}, authorizeVerbReplaceArbiter)
	if err != nil {
		t.Fatalf("parseAuthorizations: %v", err)
	}
	for _, entry := range replaceArbiterRequiredAuthorizations(auth, true, arbiterDegradedReason, arbiterSameSiteDetail) {
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
