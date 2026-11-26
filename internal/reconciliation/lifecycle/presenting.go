package lifecycle

import (
	"context"
	"maps"
	"slices"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/secrets/custody"
)

// presenting is each step's binding: its restoring apply's, else the lineage's.
type presenting struct {
	lineage string
	steps   map[string]string
}

func (p presenting) of(block string) string {
	if binding, created := p.steps[block]; created {
		return binding
	}
	return p.lineage
}

func (p presenting) bindings() []string {
	held := joinBindings(nil, p.lineage)
	for _, block := range slices.Sorted(maps.Keys(p.steps)) {
		held = joinBindings(held, p.steps[block])
	}
	slices.Sort(held)
	return held
}

func (p presenting) replaced(rebound map[string]string) presenting {
	if len(rebound) == 0 {
		return p
	}
	next := presenting{lineage: p.lineage, steps: maps.Clone(p.steps)}
	if issued, found := rebound[next.lineage]; found {
		next.lineage = issued
	}
	for block, binding := range next.steps {
		if issued, found := rebound[binding]; found {
			next.steps[block] = issued
		}
	}
	return next
}

func (p presenting) narrowed(blocks []reconciliation.Block) presenting {
	next := presenting{lineage: p.lineage}
	for _, block := range blocks {
		if binding, created := p.steps[block.ID]; created {
			if next.steps == nil {
				next.steps = map[string]string{}
			}
			next.steps[block.ID] = binding
		}
	}
	return next
}

func (l lineage) presenting() presenting {
	if len(l) == 0 {
		return presenting{}
	}
	presents := presenting{lineage: ownBinding(l[0].operation)}
	for _, holding := range l.ownership().Holdings() {
		if holding.Incarnation < 0 || !l.restores(holding.Incarnation) {
			continue
		}
		if presents.steps == nil {
			presents.steps = map[string]string{}
		}
		presents.steps[holding.Block.ID] = ownBinding(l[holding.Incarnation].operation)
	}
	for _, member := range l {
		presents = presents.replaced(member.operation.Rebound)
	}
	return presents
}

// ownBinding is the binding an apply bound for its own steps, never one its rebind issued.
func ownBinding(operation operationstore.Operation) string {
	issued := slices.Collect(maps.Values(operation.Rebound))
	for _, binding := range operation.Bindings {
		if !slices.Contains(issued, binding) {
			return binding
		}
	}
	return ""
}

func (l lineage) rebounds(from int) map[string]string {
	var joined map[string]string
	for _, member := range l[min(from, len(l)):] {
		joined = joinRebound(joined, member.operation.Rebound)
	}
	return joined
}

// replacedRebounds are the rebinds of the removals a fresh removal replaces.
func (l lineage) replacedRebounds() map[string]string {
	head := -1
	for index, member := range l {
		if member.operation.Verb == reconciliation.Apply {
			head = index
		}
	}
	return l.rebounds(head + 1)
}

func (l lineage) rebound(binding string) string {
	if issued, found := l.rebounds(0)[binding]; found {
		return issued
	}
	return binding
}

func appliedPresenting(ctx context.Context, store recordReader, applied operationstore.Operation) (presenting, error) {
	if applied.Predecessor == "" {
		return presenting{lineage: firstBinding(applied.Bindings)}, nil
	}
	held, err := appliedLineage(ctx, store, applied)
	if err != nil {
		return presenting{}, err
	}
	return held.presenting(), nil
}

// currentPresenting reads a current removal's fold, so its rebinds reach what it keeps.
func currentPresenting(ctx context.Context, store recordReader, current operationstore.Operation, plan reconciliation.Plan, states map[string]reconciliation.BlockState, owning operationstore.Operation) (presenting, error) {
	if current.Verb != reconciliation.Destroy || current.Source == "" {
		return appliedPresenting(ctx, store, owning)
	}
	folded, err := removalLineage(ctx, store, lineageMember{operation: current, plan: plan, states: states})
	if err != nil {
		return presenting{}, err
	}
	return folded.presenting(), nil
}

func withPresenting(ctx context.Context, store recordReader, decided operationPlan, operation operationstore.Operation, frozen reconciliation.Plan, states map[string]reconciliation.BlockState) (operationPlan, error) {
	if decided.fresh || decided.verb != operation.Verb || decided.operation.ID != operation.ID {
		return decided, nil
	}
	member := lineageMember{operation: operation, plan: frozen, states: states}
	switch {
	case operation.Verb == reconciliation.Apply && operation.Restores != "":
		folded, err := lineageOf(ctx, store, member)
		if err != nil {
			return decided, err
		}
		decided.presents = folded.presenting().narrowed(decided.keptSteps())
	case operation.Verb == reconciliation.Destroy && operation.Source == "":
		decided.presents = presenting{lineage: firstBinding(operation.Bindings)}
	case operation.Verb == reconciliation.Destroy:
		folded, err := removalLineage(ctx, store, member)
		if err != nil {
			return decided, err
		}
		decided.presents = folded.presenting()
		if staged(operation) {
			decided.keptBlocks = folded.keptBlocks()
		}
	}
	return decided, nil
}

func (l lineage) keptBlocks() []reconciliation.Block {
	kept := l.kept()
	var blocks []reconciliation.Block
	for _, holding := range l.ownership().Holdings() {
		if kept[holding.Block.ID] {
			blocks = append(blocks, holding.Block)
		}
	}
	return blocks
}

func (t operationPlan) restoring() bool {
	return t.restores != "" || !t.fresh && t.verb == reconciliation.Apply && t.operation.Restores != ""
}

// keptSteps are the live steps a restore or a staged destroy keeps.
func (t operationPlan) keptSteps() []reconciliation.Block {
	if !t.restoring() {
		return t.keptBlocks
	}
	var kept []reconciliation.Block
	for _, block := range t.lineagePlan.Blocks {
		if _, added := t.plan.Block(block.ID); !added {
			kept = append(kept, block)
		}
	}
	return kept
}

// presented are the lineage's binding and those presenting its plan's steps and kept Machines.
func (t operationPlan) presented() []string {
	held := joinBindings(nil, t.presents.lineage)
	for _, block := range t.plan.Blocks {
		held = joinBindings(held, t.presents.of(block.ID))
	}
	for _, block := range t.keptSteps() {
		if block.Kind == string(api.Machine) {
			held = joinBindings(held, t.presents.of(block.ID))
		}
	}
	slices.Sort(held)
	return held
}

// recorded are the bindings it presents and every one its rebinds issued.
func (t operationPlan) recorded(own ...string) []string {
	held := joinBindings(joinBindings([]string{}, own...), slices.Collect(maps.Values(t.replacements))...)
	slices.Sort(held)
	return held
}

type stepMaterial struct {
	bound map[string]map[string]secrets.Material
	steps map[string]string
	base  string
}

func (m stepMaterial) of(block string) map[string]secrets.Material {
	if binding, routed := m.steps[block]; routed {
		return m.bound[binding]
	}
	if material, found := m.bound[m.base]; found {
		return material
	}
	return map[string]secrets.Material{}
}

func (m stepMaterial) routes(block string) bool {
	_, routed := m.steps[block]
	return routed
}

func (m stepMaterial) clear() {
	for _, material := range m.bound {
		clearMaterial(material)
	}
}

func singleMaterial(material map[string]secrets.Material) stepMaterial {
	return stepMaterial{bound: map[string]map[string]secrets.Material{"": material}}
}

func (s Service) reopenEach(ctx context.Context, name string, bindings []string, into stepMaterial) (string, error) {
	for _, binding := range bindings {
		if _, reopened := into.bound[binding]; reopened || binding == "" {
			continue
		}
		bound, err := s.binder.Reopen(ctx, custody.BindingRequest{ContextName: name, BindingID: binding})
		if err != nil {
			for _, item := range bound {
				item.Material.Clear()
			}
			return binding, err
		}
		material := make(map[string]secrets.Material, len(bound))
		for _, item := range bound {
			material[item.Version.Declaration.Name] = item.Material
		}
		into.bound[binding] = material
	}
	return "", nil
}
