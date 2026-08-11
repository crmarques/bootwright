package cli

import (
	"slices"
	"strings"
	"testing"
)

const authorizationGlossLimit = 120

func TestAuthorizationsHelpPublishesEveryAcceptedToken(t *testing.T) {
	cases := []struct {
		name string
		args []string
		verb string
	}{
		{"apply", []string{"apply", "--help"}, authorizeVerbApply},
		{"plan", []string{"plan", "--help"}, authorizeVerbApply},
		{"destroy", []string{"destroy", "--help"}, authorizeVerbDestroy},
		{"replace-arbiter", []string{"storage-cluster", "replace-arbiter", "--help"}, authorizeVerbReplaceArbiter},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stdout, stderr, code := runCLI(t, tc.args...)
			if code != 0 {
				t.Fatalf("bootwright %s exited %d, stderr=%q", strings.Join(tc.args, " "), code, stderr)
			}
			glosses := authorizationsHelpGlosses(t, stdout)
			accepted := authorizationTokenNamesForVerb(tc.verb)
			for _, name := range accepted {
				gloss, ok := glosses[name]
				if !ok {
					t.Errorf("%s help does not list --authorize %s; an accepted token must be learnable from the help, not from a refusal", tc.name, name)
					continue
				}
				if len(gloss) > authorizationGlossLimit {
					t.Errorf("%s gloss for --authorize %s is %d chars, over the %d-char limit: %q", tc.name, name, len(gloss), authorizationGlossLimit, gloss)
				}
			}
			for name := range glosses {
				if !slices.Contains(accepted, name) {
					t.Errorf("%s help lists --authorize %s, which %s does not accept", tc.name, name, tc.verb)
				}
			}
		})
	}
}

func TestAuthorizationTokensCarryAShortGloss(t *testing.T) {
	for _, token := range authorizationTokens {
		if strings.TrimSpace(token.gloss) == "" {
			t.Errorf("--authorize %s has no gloss; a token published in a command's help needs its one-line meaning", token.name)
			continue
		}
		if strings.Contains(token.gloss, "\n") {
			t.Errorf("--authorize %s has a multi-line gloss; the help lists one token per line", token.name)
		}
		if len(token.gloss) > authorizationGlossLimit {
			t.Errorf("--authorize %s gloss is %d chars, over the %d-char limit", token.name, len(token.gloss), authorizationGlossLimit)
		}
	}
}

func TestUnownedNetworksAuthorizationPublishesEveryStorageConsequence(t *testing.T) {
	token, ok := authorizationTokenByName(authorizeUnownedNetworks)
	if !ok {
		t.Fatal("unowned-networks authorization is not registered")
	}
	for _, consequence := range []string{"libvirt network", "KubeVirt DataVolume", "PersistentVolumeClaim"} {
		if !strings.Contains(token.gloss, consequence) || !strings.Contains(token.authorizes, consequence) || !strings.Contains(token.inert, consequence) {
			t.Fatalf("unowned-networks does not publish %q in every operator-facing meaning: %+v", consequence, token)
		}
	}
	stdout, stderr, code := runCLI(t, "destroy", "--help")
	if code != 0 {
		t.Fatalf("destroy --help exited %d, stderr=%q", code, stderr)
	}
	if !strings.Contains(stdout, "PersistentVolumeClaim") {
		t.Fatalf("destroy help omits the PVC deletion consequence:\n%s", stdout)
	}
}

func TestAuthorizeFlagUsageNamesTokensWithoutTheirProse(t *testing.T) {
	for _, verb := range authorizationVerbs() {
		usage := flagAuthorizeUsage(verb, verb, true)
		if strings.Contains(usage, "\n") {
			t.Errorf("--authorize usage for %s spans several lines; flag usage is one line", verb)
		}
		if len(usage) > 400 {
			t.Errorf("--authorize usage for %s is %d chars; the flag entry names the tokens and leaves the prose to --help", verb, len(usage))
		}
		for _, name := range authorizationTokenNamesForVerb(verb) {
			if !strings.Contains(usage, name) {
				t.Errorf("--authorize usage for %s does not name the accepted token %s", verb, name)
			}
			token, _ := authorizationTokenByName(name)
			if token.authorizes != token.gloss && strings.Contains(usage, token.authorizes) {
				t.Errorf("--authorize usage for %s carries the full prose of %s; that prose belongs to the refusal and the spec", verb, name)
			}
		}
	}
}

func TestPlanAuthorizationHelpNamesPlanAndNoUnavailableYesFlag(t *testing.T) {
	stdout, stderr, code := runCLI(t, "plan", "--help")
	if code != 0 {
		t.Fatalf("plan --help exited %d, stderr=%q", code, stderr)
	}
	for _, want := range []string{"Tokens plan accepts:", "What each one unblocks"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("plan authorization help missing %q:\n%s", want, stdout)
		}
	}
	for _, stale := range []string{"Tokens apply accepts:", "--yes authorizes none"} {
		if strings.Contains(stdout, stale) {
			t.Fatalf("plan authorization help advertises unavailable apply surface %q:\n%s", stale, stdout)
		}
	}
}

func authorizationsHelpGlosses(t *testing.T, help string) map[string]string {
	t.Helper()
	_, block, ok := strings.Cut(help, "Authorizations:\n")
	if !ok {
		t.Fatalf("help carries no Authorizations section:\n%s", help)
	}
	if before, _, cut := strings.Cut(block, "\nUsage:"); cut {
		block = before
	}
	glosses := map[string]string{}
	for _, line := range strings.Split(block, "\n") {
		if !strings.HasPrefix(line, "    ") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		if _, known := authorizationTokenByName(fields[0]); !known {
			continue
		}
		glosses[fields[0]] = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), fields[0]))
	}
	return glosses
}
