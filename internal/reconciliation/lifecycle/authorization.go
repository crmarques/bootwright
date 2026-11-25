package lifecycle

import (
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"strconv"
	"strings"

	"github.com/crmarques/bootwright/internal/reconciliation"
)

func (s Service) authorized(name string, decided operationPlan, authorizations []string, unattended bool) error {
	if err := authorize(name, decided.verb, decided.floor(), decided.selection, decided.plan, decided.authorizationPlan(), authorizations, unattended); err != nil {
		return err
	}
	if err := s.refuseUnattendedAbandonment(name, unattended); err != nil {
		return err
	}
	return s.acknowledgedReleases(name, decided)
}

// authorize compares the tokens this invocation supplied with the tokens the
// reachable apply effects or the frozen removal plan consume. A missing token refuses before
// registration, so an irreversible consequence is always acknowledged first; a
// token the plan does not consume refuses too, so a habitual authorization
// cannot pre-authorize a future destructive plan. Either refusal names the
// consuming steps by their place in the presented plan and the exact command
// that passes, which keeps the Machines the invocation names unregistered.
// An unattended invocation must bind each token to its steps' digest (Q117).
func authorize(contextName string, verb reconciliation.Verb, floor reconciliation.Stage, selection reconciliation.Selection, plan, scoped reconciliation.Plan, authorizations []string, unattended bool) error {
	consumers := map[string][]string{}
	required := requiredTokens(scoped)
	digests := map[string]string{}
	named := make([]string, 0, len(authorizations))
	for _, supplied := range authorizations {
		token, digest, _ := strings.Cut(supplied, "=")
		named = append(named, token)
		digests[token] = digest
	}
	authorizations = named
	for index, block := range plan.Blocks {
		if len(block.Consumes) == 0 {
			continue
		}
		if _, included := scoped.Block(block.ID); !included {
			continue
		}
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
	selected := slices.Concat(relaxationFlags(frozenRelaxations(plan)), unregisteredFlags(plan))
	passing := contextCommand(contextName, string(verb), invocationFlags(floor, selection, append(authFlags, selected...))...)
	preview := contextCommand(contextName, string(verb)+" --dry-run", invocationFlags(floor, selection, selected)...)
	supplied := slices.Compact(slices.Sorted(slices.Values(authorizations)))
	for _, token := range supplied {
		if !slices.Contains(required, token) {
			return authorizeUnneeded.Fail(
				"this plan requires no "+token+" authorization",
				"repeat "+passing+" without --authorize "+token)
		}
	}
	for _, token := range required {
		if !slices.Contains(supplied, token) {
			return authorizeMissing.Fail(
				"this plan has "+token+" consequences that are not authorized: "+strings.Join(consumers[token], ", "),
				"repeat it with the authorization: "+passing)
		}
		expected := AuthorizationDigest(scoped, token)
		switch digest := digests[token]; {
		case digest != "" && digest != expected:
			return authorizeStale.Fail(
				"--authorize "+token+"="+digest+" names steps this plan no longer holds: its "+token+" steps are now "+strings.Join(consumers[token], ", "),
				"review them with "+preview+", then repeat "+passing)
		case digest == "" && unattended:
			return authorizeStale.Fail(
				"an unattended "+string(verb)+" must bind --authorize "+token+" to the steps it authorizes: "+strings.Join(consumers[token], ", "),
				"review them with "+preview+", then repeat "+passing)
		}
	}
	return nil
}

// invocationFlags are the flags a remedy repeats: a destroy's floor, stage, targets, until, then authorizations.
func invocationFlags(floor reconciliation.Stage, selection reconciliation.Selection, authorizing []string) []string {
	var flags []string
	if floor != "" {
		flags = append(flags, "--stage", string(floor))
	}
	if len(selection.Stages) != 0 {
		flags = append(flags, "--stage", strings.Join(selection.Stages.Names(), ","))
	}
	if len(selection.Clusters) != 0 {
		flags = append(flags, "--clusters", strings.Join(selection.Clusters, ","))
	}
	if len(selection.Machines) != 0 {
		flags = append(flags, "--machines", strings.Join(selection.Machines, ","))
	}
	if selection.Until != "" {
		flags = append(flags, "--until", selection.Until)
	}
	return append(flags, authorizing...)
}

// AuthorizationDigest hashes the identity and digests of a token's steps, in frozen order.
func AuthorizationDigest(plan reconciliation.Plan, token string) string {
	sum := sha256.New()
	sum.Write([]byte("bootwright.authorization-v1\x00" + token))
	if plan.Verb == reconciliation.Apply {
		sum.Write([]byte("\x00apply"))
	} else {
		for _, block := range plan.Blocks {
			sum.Write([]byte("\x00removes\x00" + block.ID))
		}
	}
	for _, block := range plan.Blocks {
		if slices.Contains(block.Consumes, token) {
			sum.Write([]byte("\x00" + block.ID + "\x00" + block.Kind + "\x00" + block.Object + "\x00" + block.Implementation +
				"\x00" + block.ContentDigest + "\x00" + block.RequestDigest))
			if block.Relaxation != "" {
				sum.Write([]byte("\x00relaxation\x00" + block.Relaxation))
			}
		}
	}
	return hex.EncodeToString(sum.Sum(nil))[:authorizationDigestLength]
}
