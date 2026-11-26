package lifecycle

import (
	"context"
	"maps"
	"slices"
	"strconv"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/secrets/custody"
	"github.com/crmarques/bootwright/internal/secrets/secretstore"
)

const RebindAuthorization = "rebind="

func splitRebinds(contextName string, decided operationPlan, authorizations []string) ([]string, []string, error) {
	var tokens, rebinds []string
	for _, token := range authorizations {
		secret, rebind := strings.CutPrefix(token, RebindAuthorization)
		if !rebind {
			tokens = append(tokens, token)
			continue
		}
		if !api.ValidLexical(api.LexicalName, secret) || slices.Contains(rebinds, secret) {
			return nil, nil, authorizeRebindInvalid.Fail(
				"--authorize "+token+" must name one Secret once",
				"repeat "+contextCommand(contextName, string(decided.verb), invocationFlags(decided.floor(), reconciliation.Selection{}, nil)...)+" naming each rotated access credential once as --authorize rebind=<secret>")
		}
		rebinds = append(rebinds, secret)
	}
	slices.Sort(rebinds)
	return tokens, rebinds, nil
}

func (s Service) admitRebinds(ctx context.Context, name string, decided operationPlan, rebinds []string) (operationPlan, error) {
	if len(rebinds) == 0 {
		return decided, nil
	}
	without := "repeat " + contextCommand(name, string(decided.verb), invocationFlags(decided.floor(), reconciliation.Selection{}, nil)...) + " without --authorize " + RebindAuthorization
	if len(frozenBindings(decided)) == 0 {
		return operationPlan{}, authorizeRebindInvalid.Fail(
			"this "+string(decided.verb)+" reopens no Secret binding, so no Secret is rebound", without+rebinds[0])
	}
	current, err := s.binder.ReadCurrent(ctx, custody.ReadCurrentRequest{ContextName: name, Names: rebinds})
	if err != nil {
		return operationPlan{}, err
	}
	currentVersions := map[string]string{}
	for _, item := range current {
		currentVersions[item.Version.Declaration.Name] = item.Version.ID
		item.Material.Clear()
	}
	pins := map[string]map[string]string{}
	frozen := frozenBindings(decided)
	for _, binding := range frozen {
		if pins[binding], err = s.pinnedVersions(ctx, name, binding); err != nil {
			return operationPlan{}, err
		}
	}
	for _, secret := range rebinds {
		if !slices.ContainsFunc(frozen, func(binding string) bool { _, held := pins[binding][secret]; return held }) {
			return operationPlan{}, authorizeRebindInvalid.Fail(
				"the Secret binding "+strings.Join(frozen, ", ")+" this "+string(decided.verb)+" reopens pins no version of Secret "+secret, without+secret)
		}
		targets, err := s.rebindTargets(decided)
		if err != nil {
			return operationPlan{}, err
		}
		presenting, err := s.presentedOnly(decided, targets, secret, without+secret)
		if err != nil {
			return operationPlan{}, err
		}
		stale := false
		for _, binding := range presenting {
			pinned, held := pins[binding][secret]
			if !held {
				return operationPlan{}, authorizeRebindInvalid.Fail(
					"the Secret binding "+binding+" this "+string(decided.verb)+" reopens pins no version of Secret "+secret, without+secret)
			}
			stale = stale || pinned != currentVersions[secret]
		}
		if !stale {
			return operationPlan{}, authorizeRebindInvalid.Fail(
				"Secret "+secret+" is already presented at its current version, so there is nothing to rebind", without+secret)
		}
	}
	decided.rebinds = rebinds
	return decided, nil
}

func (s Service) pinnedVersions(ctx context.Context, name, binding string) (map[string]string, error) {
	reopened, err := s.binder.Reopen(ctx, custody.BindingRequest{ContextName: name, BindingID: binding})
	for _, item := range reopened {
		item.Material.Clear()
	}
	if err != nil {
		return nil, err
	}
	pinned := make(map[string]string, len(reopened))
	for _, item := range reopened {
		pinned[item.Version.Declaration.Name] = item.Version.ID
	}
	return pinned, nil
}

// rebindTargets are a restore's kept Machines it proves off or acts on, with a continued restore's own steps, else the plan's steps with a staged destroy's kept power targets.
func (s Service) rebindTargets(decided operationPlan) ([]reconciliation.Block, error) {
	if !decided.restoring() {
		return append(slices.Clone(decided.plan.Blocks), decided.keptPowerBlocks()...), nil
	}
	var targets []reconciliation.Block
	if !decided.fresh {
		targets = slices.Clone(decided.plan.Blocks)
	}
	kept, err := s.powerTargets(decided)
	for _, block := range kept {
		if _, added := decided.plan.Block(block.ID); !added {
			targets = append(targets, block)
		}
	}
	return targets, err
}

func (t operationPlan) keptPowerBlocks() []reconciliation.Block {
	stage := t.stage
	if stage == nil && !t.fresh && staged(t.operation) {
		stage = t.operation.Stage
	}
	if stage == nil {
		return nil
	}
	var blocks []reconciliation.Block
	for _, block := range t.keptSteps() {
		_, planned := t.plan.Block(block.ID)
		if !planned && slices.ContainsFunc(stage.KeptPower, func(target operationstore.KeptPower) bool { return target.Block == block.ID }) {
			blocks = append(blocks, block)
		}
	}
	return blocks
}

func stepName(decided operationPlan, block reconciliation.Block) string {
	if index := slices.IndexFunc(decided.plan.Blocks, func(held reconciliation.Block) bool { return held.ID == block.ID }); index >= 0 {
		return "step " + strconv.Itoa(index+1) + " (" + block.Description + ")"
	}
	return "kept step " + block.ID + " (" + block.Description + ")"
}

// presentedOnly admits only an access credential of each target and kept step whose binding it replaces.
func (s Service) presentedOnly(decided operationPlan, targets []reconciliation.Block, secret, remedy string) ([]string, error) {
	var presented, refused, bindings []string
	for _, block := range targets {
		step := stepName(decided, block)
		access, used, err := s.accessOnly(block, step, secret, remedy)
		switch {
		case err != nil:
			return nil, err
		case !used:
		case access:
			presented = append(presented, step)
			bindings = joinBindings(bindings, decided.presenter(block.ID))
		default:
			refused = append(refused, step)
		}
	}
	for _, block := range decided.keptSteps() {
		if slices.ContainsFunc(targets, func(target reconciliation.Block) bool { return target.ID == block.ID }) || !slices.Contains(bindings, decided.presenter(block.ID)) {
			continue
		}
		step := stepName(decided, block)
		access, used, err := s.accessOnly(block, step, secret, remedy)
		if err != nil {
			return nil, err
		}
		if used && !access {
			refused = append(refused, step)
		}
	}
	if len(refused) != 0 {
		return nil, authorizeRebindInvalid.Fail(
			"Secret "+secret+" is not only an access credential this "+string(decided.verb)+" presents: "+strings.Join(refused, ", ")+
				" rest on the version its operation bound, which alone proves what they created", remedy)
	}
	switch {
	case len(presented) == 0 && decided.fresh && decided.restores != "":
		return nil, authorizeRebindInvalid.Fail(
			"no kept Machine this restoring apply proves off or acts on presents Secret "+secret+", and it binds the current version of every Secret its added steps name, so no Secret is rebound", remedy)
	case len(presented) == 0:
		return nil, authorizeRebindInvalid.Fail(
			"no step of this "+string(decided.verb)+" presents Secret "+secret, remedy)
	}
	slices.Sort(bindings)
	return bindings, nil
}

func (s Service) accessOnly(block reconciliation.Block, step, secret, remedy string) (bool, bool, error) {
	capability, used, err := s.referencing(block, step, secret, remedy)
	if err != nil || !used {
		return false, used, err
	}
	var classes secrets.Classes
	if classifier, ok := capability.(SecretClassifier); ok {
		if classes, err = classifier.SecretClasses(block); err != nil {
			return false, true, err
		}
	}
	return classes.Of(secret) == secrets.AccessCredential, true, nil
}

func (s Service) referencing(block reconciliation.Block, step, secret, remedy string) (Capability, bool, error) {
	capability, found := s.capabilities.Resolve(block.Kind, block.Implementation)
	referencer, named := capability.(SecretReferencer)
	if !found || !named {
		return nil, false, authorizeRebindInvalid.Fail(
			step+" has a capability that cannot name the Secrets it references, so no Secret is rebound", remedy)
	}
	references, err := referencer.SecretReferences(block)
	return capability, slices.Contains(references, secret), err
}

func (s Service) rebind(ctx context.Context, name string, decided operationPlan) (operationPlan, error) {
	if len(decided.rebinds) == 0 {
		return decided, nil
	}
	named := map[string][]string{}
	targets, err := s.rebindTargets(decided)
	if err != nil {
		return operationPlan{}, err
	}
	for _, block := range targets {
		capability, found := s.capabilities.Resolve(block.Kind, block.Implementation)
		referencer, names := capability.(SecretReferencer)
		if !found || !names {
			continue
		}
		references, err := referencer.SecretReferences(block)
		if err != nil {
			return operationPlan{}, err
		}
		binding := decided.presenter(block.ID)
		for _, secret := range decided.rebinds {
			if slices.Contains(references, secret) && !slices.Contains(named[binding], secret) {
				named[binding] = append(named[binding], secret)
			}
		}
	}
	replaced := map[string]string{}
	for _, binding := range slices.Sorted(maps.Keys(named)) {
		request := custody.RebindRequest{ContextName: name, BindingID: binding, Names: slices.Sorted(slices.Values(named[binding]))}
		var issued secretstore.Binding
		var err error
		if s.startCheck != nil {
			issued, err = s.admittedRebind(ctx, request)
		} else {
			issued, err = s.binder.Rebind(ctx, request)
		}
		if err != nil {
			return operationPlan{}, beside(err, s.releaseIssued(ctx, name, decided.rebound))
		}
		decided.rebound = append(decided.rebound, rebinding{replaced: binding, binding: issued.ID, secrets: request.Names})
		replaced[binding] = issued.ID
	}
	if decided.verb != reconciliation.Destroy && !decided.fresh {
		bindings := slices.Clone(decided.operation.Bindings)
		for index, binding := range bindings {
			if issued, found := replaced[binding]; found {
				bindings[index] = issued
			}
		}
		if decided.restoring() {
			kept := maps.Clone(replaced)
			for _, binding := range decided.operation.Bindings {
				delete(kept, binding)
			}
			decided.presents = decided.presents.replaced(replaced)
			decided.operation.Rebound = joinRebound(decided.operation.Rebound, kept)
			bindings = joinBindings(bindings, slices.Collect(maps.Values(kept))...)
		}
		slices.Sort(bindings)
		decided.operation.Bindings = bindings
		return decided, nil
	}
	decided.presents = decided.presents.replaced(replaced)
	decided.replacements = joinRebound(decided.replacements, replaced)
	if decided.verb != reconciliation.Destroy {
		return decided, nil
	}
	decided.reopen = decided.presents.lineage
	if !decided.fresh {
		decided.operation.Rebound = joinRebound(decided.operation.Rebound, replaced)
		decided.replacements = decided.operation.Rebound
		decided.operation.Bindings = decided.recorded(decided.presented()...)
	}
	return decided, nil
}

func (s Service) releaseIssued(ctx context.Context, name string, issued rebindings) error {
	for _, binding := range issued.issued() {
		if _, err := s.binder.Release(recordingContext(ctx), custody.BindingRequest{ContextName: name, BindingID: binding}); err != nil {
			return err
		}
	}
	return nil
}

func joinRebound(earlier, replaced map[string]string) map[string]string {
	if len(earlier) == 0 && len(replaced) == 0 {
		return nil
	}
	joined := maps.Clone(earlier)
	if joined == nil {
		joined = map[string]string{}
	}
	for from, to := range joined {
		if next, found := replaced[to]; found {
			joined[from] = next
		}
	}
	for from, to := range replaced {
		if _, found := joined[from]; !found {
			joined[from] = to
		}
	}
	return joined
}

type rebinding struct {
	replaced string
	binding  string
	secrets  []string
}

func (r rebinding) record() operationstore.LogRecord {
	return operationstore.LogRecord{Event: "rebind",
		Detail: "Secret binding " + r.replaced + " rebound as " + r.binding + " at the current version of " + strings.Join(r.secrets, ", ")}
}

type rebindings []rebinding

func (r rebindings) issued() []string {
	var issued []string
	for _, rebound := range r {
		issued = append(issued, rebound.binding)
	}
	return issued
}

func (s Service) admittedRebind(ctx context.Context, request custody.RebindRequest) (secretstore.Binding, error) {
	binder, ok := s.binder.(interface {
		PrepareRebind(context.Context, custody.RebindRequest) (custody.ScopedRebind, error)
	})
	if !ok {
		return secretstore.Binding{}, engineRequestDefect.Fail("the credential store cannot rebind inside the admitted transaction", "")
	}
	issue, err := binder.PrepareRebind(ctx, request)
	if err != nil {
		return secretstore.Binding{}, err
	}
	var issued secretstore.Binding
	err = s.mutateAdmitted(ctx, request.ContextName, s.startCheck, func(tx Transaction) error {
		return tx.Secrets(ctx, func(selected secretstore.Context, area secretstore.Area) error {
			var err error
			issued, err = issue(ctx, selected, area)
			return err
		})
	})
	return issued, err
}
