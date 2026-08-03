package cli

import (
	"slices"
	"testing"

	"github.com/crmarques/bootwright/internal/converge"
	"github.com/crmarques/bootwright/internal/converge/workflow"
)

func forecastTokensForVerb(t *testing.T, verb string) []string {
	t.Helper()
	auth, err := parseAuthorizations(nil, verb)
	if err != nil {
		t.Fatalf("parseAuthorizations(%q): %v", verb, err)
	}
	var entries []requiredAuthorization
	switch verb {
	case authorizeVerbDestroy:
		entries = destroyRequiredAuthorizations(auth, destroyGateForecast{
			runScope:            converge.AllScope,
			staleInput:          true,
			unreadableRecords:   true,
			sharedInfra:         true,
			installedNode:       true,
			protected:           true,
			protectedReason:     "an Environment spec.safety rule protects the selection",
			dataLoss:            true,
			dataLossReason:      "the teardown wipes node disks",
			unreadableRecordDir: t.TempDir(),
		})
	case authorizeVerbApply:
		state := loadFixtureState(t, "006-ceph-3nodes-libvirt-managed-os")
		tasks := planApplyTasks(t, converge.AllScope.ApplyTarget(), state)
		entries = applyRequiredAuthorizations(auth, workflow.ApplyModeRebuild, state, state, tasks, t.TempDir(), t.TempDir(), []string{"ceph-storage"}, "all")
	case authorizeVerbReplaceArbiter:
		entries = replaceArbiterRequiredAuthorizations(auth, true, "mon(s) mon-b are outside quorum", "mon.mon-c shares its site with mon-a", true)
	default:
		t.Fatalf("verb %q has no forecast builder in this guard; add one with the verb", verb)
	}
	tokens := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !slices.Contains(tokens, entry.Token) {
			tokens = append(tokens, entry.Token)
		}
	}
	return tokens
}

func TestEveryTokenAVerbAcceptsIsNamedByItsPreviewForecast(t *testing.T) {
	for _, verb := range authorizationVerbs() {
		t.Run(verb, func(t *testing.T) {
			forecast := forecastTokensForVerb(t, verb)
			for _, token := range authorizationTokenNamesForVerb(verb) {
				if token == authorizeAll {
					continue
				}
				if !slices.Contains(forecast, token) {
					t.Errorf("%s accepts --authorize %s, but a preview reaching every gate never names it; a token published for a verb must appear in that verb's required-authorizations forecast, or the preview and the run disagree about what the run consults", verb, token)
				}
			}
		})
	}
}

func TestAPreviewForecastNamesNoTokenItsVerbWouldReject(t *testing.T) {
	for _, verb := range authorizationVerbs() {
		t.Run(verb, func(t *testing.T) {
			accepted := authorizationTokenNamesForVerb(verb)
			for _, token := range forecastTokensForVerb(t, verb) {
				if !slices.Contains(accepted, token) {
					t.Errorf("the %s preview forecasts --authorize %s, but %s rejects that token; a preview must never ask for an authorization its own verb refuses to parse", verb, token, verb)
				}
			}
		})
	}
}
