package lifecycle

import (
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"strconv"
	"strings"

	"github.com/crmarques/bootwright/internal/reconciliation"
)

func (s Service) authorized(name string, decided operationPlan, authorizations, unregistered []string, unattended bool) error {
	if err := authorize(name, decided.verb, decided.selection, decided.plan, authorizations, unregistered, unattended); err != nil {
		return err
	}
	return s.acknowledgedReleases(name, decided.plan, unregistered)
}

// authorize compares the tokens this invocation supplied with the tokens the
// whole frozen plan's blocks consume. A missing token refuses before
// registration, so an irreversible consequence is always acknowledged first; a
// token the plan does not consume refuses too, so a habitual authorization
// cannot pre-authorize a future destructive plan. Either refusal names the
// consuming steps by their place in the presented plan and the exact command
// that passes, which keeps the Machines the invocation names unregistered.
// An unattended invocation must bind each token to its steps' digest (Q117).
func authorize(contextName string, verb reconciliation.Verb, selection reconciliation.Selection, plan reconciliation.Plan, authorizations, unregistered []string, unattended bool) error {
	consumers := map[string][]string{}
	required := requiredTokens(plan)
	digests := map[string]string{}
	named := make([]string, 0, len(authorizations))
	for _, supplied := range authorizations {
		token, digest, _ := strings.Cut(supplied, "=")
		named = append(named, token)
		digests[token] = digest
	}
	authorizations = named
	for index, block := range plan.Blocks {
		for _, token := range block.Consumes {
			consumers[token] = append(consumers[token], "step "+strconv.Itoa(index+1)+" ("+block.Description+")")
		}
	}
	var authFlags []string
	for _, token := range required {
		if unattended {
			token += "=" + DigestPlaceholder
		}
		authFlags = append(authFlags, "--authorize", token)
	}
	passing := contextCommand(contextName, string(verb), invocationFlags(selection, authFlags, unregistered)...)
	supplied := slices.Compact(slices.Sorted(slices.Values(authorizations)))
	for _, token := range supplied {
		if !slices.Contains(required, token) {
			return failure("lifecycle.authorization",
				"this plan requires no "+token+" authorization",
				"repeat "+passing+" without --authorize "+token)
		}
	}
	for _, token := range required {
		if !slices.Contains(supplied, token) {
			return failure("lifecycle.authorization",
				"this plan has "+token+" consequences that are not authorized: "+strings.Join(consumers[token], ", "),
				"repeat it with the authorization: "+passing)
		}
		expected := AuthorizationDigest(plan, token)
		switch digest := digests[token]; {
		case digest != "" && digest != expected:
			return failure("lifecycle.authorization",
				"--authorize "+token+"="+digest+" names steps this plan no longer holds: its "+token+" steps are now "+strings.Join(consumers[token], ", "),
				"review them with "+contextCommand(contextName, "plan")+", then repeat "+passing)
		case digest == "" && unattended:
			return failure("lifecycle.authorization",
				"an unattended "+string(verb)+" must bind --authorize "+token+" to the steps it authorizes: "+strings.Join(consumers[token], ", "),
				"review them with "+contextCommand(contextName, "plan")+", then repeat "+passing)
		}
	}
	return nil
}

// invocationFlags are the flags a remedy repeats: stage, until, authorizations, unregistered.
func invocationFlags(selection reconciliation.Selection, authorizing, unregistered []string) []string {
	var flags []string
	if len(selection.Stages) != 0 {
		flags = append(flags, "--stage", strings.Join(selection.Stages.Names(), ","))
	}
	if selection.Until != "" {
		flags = append(flags, "--until", selection.Until)
	}
	flags = append(flags, authorizing...)
	if len(unregistered) != 0 {
		flags = append(flags, "--unregistered", strings.Join(slices.Compact(slices.Sorted(slices.Values(unregistered))), ","))
	}
	return flags
}

// AuthorizationDigest hashes the identity and digests of a token's steps, in frozen order.
func AuthorizationDigest(plan reconciliation.Plan, token string) string {
	sum := sha256.New()
	sum.Write([]byte("bootwright.authorization-v1\x00" + token))
	for _, block := range plan.Blocks {
		if slices.Contains(block.Consumes, token) {
			sum.Write([]byte("\x00" + block.ID + "\x00" + block.Kind + "\x00" + block.Object + "\x00" + block.Implementation +
				"\x00" + block.ContentDigest + "\x00" + block.RequestDigest))
		}
	}
	return hex.EncodeToString(sum.Sum(nil))[:16]
}
