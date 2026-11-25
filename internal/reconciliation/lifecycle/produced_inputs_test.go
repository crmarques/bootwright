package lifecycle

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/secrets/custody"
	"github.com/crmarques/bootwright/internal/secrets/secretstore"
)

type lendingCapability struct {
	*testCapability
	references map[string][]ProducedInput
	borrowed   []string
	held       []secrets.Material
}

func (c *lendingCapability) ProducedInputs(block reconciliation.Block) ([]ProducedInput, error) {
	return slices.Clone(c.references[block.ID]), nil
}
func (c *lendingCapability) ProducedOutputs(block reconciliation.Block) ([]string, error) {
	if strings.HasPrefix(block.ID, "producer") {
		return []string{"access"}, nil
	}
	return nil, nil
}
func (c *lendingCapability) borrow(ctx context.Context, execution Execution) error {
	for _, ref := range c.references[execution.Block.ID] {
		material, err := execution.LendProduced(ctx, ref)
		if err != nil {
			return err
		}
		c.held = append(c.held, material)
		c.borrowed = append(c.borrowed, materialText(material))
	}
	return nil
}
func (c *lendingCapability) Apply(ctx context.Context, e Execution) (Result, error) {
	if err := c.borrow(ctx, e); err != nil {
		return Result{Outcome: reconciliation.OutcomeFailed}, err
	}
	return c.testCapability.Apply(ctx, e)
}
func (c *lendingCapability) Destroy(ctx context.Context, e Execution) (Result, error) {
	if err := c.borrow(ctx, e); err != nil {
		return Result{Outcome: reconciliation.OutcomeFailed}, err
	}
	return c.testCapability.Destroy(ctx, e)
}
func (c *lendingCapability) Observe(ctx context.Context, e Execution) (Observation, error) {
	if err := c.borrow(ctx, e); err != nil {
		return Observation{Effect: reconciliation.EffectUnknown}, err
	}
	return c.testCapability.Observe(ctx, e)
}

func (b *testBinder) ReadProducedInArea(ctx context.Context, selected secretstore.Context, area secretstore.Area, block, name string) (secretstore.ProducedMaterial, bool, error) {
	if err := lendLent(area); err != nil {
		return secretstore.ProducedMaterial{}, false, err
	}
	return b.ReadProduced(ctx, custody.ReadProducedRequest{ContextName: selected.Name, Block: block, Name: name})
}
func (b *testBinder) ReadProduced(ctx context.Context, request custody.ReadProducedRequest) (secretstore.ProducedMaterial, bool, error) {
	if err := ctx.Err(); err != nil {
		return secretstore.ProducedMaterial{}, false, err
	}
	if request.ContextName != testContextName {
		return secretstore.ProducedMaterial{}, false, errors.New("wrong context")
	}
	b.producedReads++
	key := request.Block + "/" + request.Name
	if err := b.readErr[key]; err != nil {
		return secretstore.ProducedMaterial{}, false, err
	}
	value, found := b.produced[key]
	return secretstore.ProducedMaterial{Material: secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte(value)}), Unproved: b.unproved[key], Identity: b.identities[key]}, found, nil
}

func lendingHarness(t *testing.T) (*harness, *lendingCapability) {
	t.Helper()
	h := newHarness(t, "producer-a", "producer-b", "consumer")
	for i := range h.capability.definitions {
		h.capability.definitions[i].Object = h.capability.definitions[i].ID
	}
	refs := []ProducedInput{{Kind: h.capability.definitions[0].Kind, Object: "producer-a", Output: "access"}, {Kind: h.capability.definitions[1].Kind, Object: "producer-b", Output: "access"}}
	c := &lendingCapability{testCapability: h.capability, references: map[string][]ProducedInput{"consumer": refs}}
	h.service.capabilities = testResolver{capability: c}
	h.service.options.Concurrency = 1
	h.capability.outcomeFor = map[string]Result{}
	for _, name := range []string{"producer-a", "producer-b"} {
		h.capability.outcomeFor[name] = Result{Outcome: reconciliation.OutcomeChanged, Evidence: []byte(`{"ok":true}`), Produced: []Produced{{Name: "access", Material: secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte(name + "-private-material")})}}}
	}
	h.capability.outcomeFor["consumer"] = Result{Outcome: reconciliation.OutcomeChanged, Evidence: []byte(`{"ok":true}`)}
	return h, c
}

func TestProducedInputsLendSeveralProducersAcrossApplyAndRemoval(t *testing.T) {
	t.Parallel()
	h, c := lendingHarness(t)
	if _, err := apply(h); err != nil {
		t.Fatal(err)
	}
	want := []string{"producer-a-private-material", "producer-b-private-material"}
	if !slices.Equal(c.borrowed, want) {
		t.Fatalf("borrowed %v", c.borrowed)
	}
	for _, material := range c.held {
		if strings.Trim(materialText(material), "\x00") != "" {
			t.Fatal("attempt retained produced material")
		}
	}
	for _, name := range []string{"producer-a", "producer-b"} {
		h.capability.outcomeFor[name] = Result{Outcome: reconciliation.OutcomeChanged, Evidence: []byte(`{"ok":true}`)}
	}
	if _, err := destroy(h); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(c.borrowed, append(slices.Clone(want), want...)) {
		t.Fatal(c.borrowed)
	}
	for _, data := range h.workspace.area.files {
		if strings.Contains(string(data), "private-material") {
			t.Fatal("produced material reached a record")
		}
	}
}

func TestProducedInputLenderRefusesMissingUnprovedUndeclaredAndExpired(t *testing.T) {
	t.Parallel()
	h, c := lendingHarness(t)
	definitions := h.capability.definitions
	block := reconciliation.Block{BlockDefinition: definitions[2]}
	for _, tc := range []struct {
		name                  string
		found, unproved, want bool
		removing              map[string]bool
	}{
		{"missing", false, false, false, nil}, {"unproved apply", true, true, false, nil},
		{"unproved removal", true, true, true, map[string]bool{"producer-a": true}},
		{"unproved kept by a removal", true, true, false, map[string]bool{"consumer": true}},
		{"proved", true, false, true, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lend, release, err := h.service.producedLender(block, definitions, tc.removing, func(context.Context, string, string) (secretstore.ProducedMaterial, bool, error) {
				return secretstore.ProducedMaterial{Material: secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte("private")}), Unproved: tc.unproved}, tc.found, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			material, err := lend(context.Background(), c.references["consumer"][0])
			if (err == nil) != tc.want {
				t.Fatalf("lend: %v", err)
			}
			if _, err := lend(context.Background(), ProducedInput{Kind: "Other", Object: "other", Output: "access"}); err == nil {
				t.Fatal("lent undeclared input")
			}
			release()
			if strings.Trim(materialText(material), "\x00") != "" {
				t.Fatal("release retained bytes")
			}
			if _, err := lend(context.Background(), c.references["consumer"][0]); err == nil {
				t.Fatal("lent expired input")
			}
		})
	}
}

func materialText(material secrets.Material) string {
	value, _ := material.Part(secrets.ValuePart)
	defer clear(value)
	return string(value)
}

func TestProducedInputContinuationReopensAndWithholdsEachUnprovedEntry(t *testing.T) {
	t.Parallel()
	for _, entry := range []string{"", "producer-a/access", "producer-b/access", "missing"} {
		t.Run(entry, func(t *testing.T) {
			h, c := lendingHarness(t)
			h.capability.errorFor = map[string]error{"consumer": errors.New("scripted interruption")}
			if _, err := apply(h); err == nil {
				t.Fatal("scripted apply passed")
			}
			h.capability.errorFor = nil
			if entry == "missing" {
				delete(h.binder.produced, "producer-a/access")
			} else if entry != "" {
				h.binder.unproved[entry] = true
			}
			_, err := apply(h)
			if (err == nil) != (entry == "") {
				t.Fatalf("continuation with %q: %v", entry, err)
			}
			for _, material := range c.held {
				if strings.Trim(materialText(material), "\x00") != "" {
					t.Fatal("failed continuation retained lent bytes")
				}
			}
			if entry != "" && entry != "missing" {
				if _, err := destroy(h); err != nil {
					t.Fatalf("removal could not borrow unproved entry: %v", err)
				}
			}
		})
	}
}

type probingLender struct{ *lendingCapability }

func (c probingLender) Quiescent(ctx context.Context, probe Probe) (Quiescence, error) {
	if err := c.borrow(ctx, probe.Execution()); err != nil {
		return Quiescence{}, err
	}
	return c.testCapability.Quiescent(ctx, probe)
}

func (c probingLender) ObserveRemoval(ctx context.Context, e Execution) (Observation, error) {
	if err := c.borrow(ctx, e); err != nil {
		return Observation{Effect: reconciliation.EffectUnknown}, err
	}
	return c.testCapability.ObserveRemoval(ctx, e)
}

// A staged destroy over one completed apply lends the consumer it takes back
// the entries of the producers it keeps, to its quiescence probe, its removal
// attempt and the resolution that settles that attempt, and keeps them.
func TestAStagedDestroyLendsTheEntriesOfTheProducersItKeeps(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h, c := lendingHarness(t)
	h.capability.definitions[2].Stage = reconciliation.StageClusters
	h.service.capabilities = testResolver{capability: probingLender{c}}
	if _, err := apply(h); err != nil {
		t.Fatal(err)
	}
	want := []string{"producer-a-private-material", "producer-b-private-material"}
	c.borrowed = nil
	h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeUnknown}}
	request := DestroyRequest{ContextName: testContextName, Floor: string(reconciliation.StageClusters), SkipConfirmation: true}
	if _, err := h.service.Destroy(ctx, request); firstCode(err) == planCapabilityDefect.Code || !slices.Equal(c.borrowed, append(slices.Clone(want), want...)) {
		t.Fatalf("the staged destroy borrowed %v (%v)", c.borrowed, diagnostics.Of(err))
	}
	h.capability.outcomes, h.capability.observations, c.borrowed = nil, []Observation{{Effect: reconciliation.EffectCompleted}}, nil
	if _, err := h.service.Destroy(ctx, request); err != nil || !slices.Equal(c.borrowed, want) {
		t.Fatalf("the resolution borrowed %v (%v)", c.borrowed, diagnostics.Of(err))
	}
	if removal, plan := currentRecord(t, h); !trimmed(removal) || !slices.Equal(planIDs(plan), []string{"consumer"}) {
		t.Fatalf("the staged destroy recorded %+v over %v", removal, planIDs(plan))
	}
	if held := kept(h); len(held) != 2 {
		t.Fatalf("the staged destroy left custody %v", held)
	}
}

// A removal lends from the blocks its plan takes back and those the context
// keeps, never one its frozen taken-back set names, and a whole destroy keeps
// none; only a block it takes back may lend an unproved entry.
func TestARemovalLendsFromWhatItTakesBackAndWhatTheContextKeeps(t *testing.T) {
	t.Parallel()
	blocks := func(ids ...string) []reconciliation.Block {
		var held []reconciliation.Block
		for _, id := range ids {
			held = append(held, reconciliation.Block{BlockDefinition: definition(id)})
		}
		return held
	}
	applied := operationstore.Operation{ID: "op-applied", Verb: reconciliation.Apply, State: reconciliation.OperationDone}
	held := lineage{{operation: applied, plan: reconciliation.Plan{Verb: reconciliation.Apply, Blocks: blocks("producer-a", "producer-b", "consumer")}}}
	removal := reconciliation.Plan{Verb: reconciliation.Destroy, Blocks: blocks("consumer")}
	for name, tc := range map[string]struct {
		stage *operationstore.Stage
		want  []string
	}{
		"staged": {&operationstore.Stage{Floor: reconciliation.StageClusters, TakenBack: []string{"consumer", "producer-b"}}, []string{"consumer", "producer-a"}},
		"whole":  {nil, []string{"consumer"}},
	} {
		definitions, removing := held.removalLending(removal, tc.stage)
		var ids []string
		for _, definition := range definitions {
			ids = append(ids, definition.ID)
		}
		if !slices.Equal(ids, tc.want) || len(removing) != 1 || !removing["consumer"] {
			t.Fatalf("a %s removal lends from %v, unproved from %v", name, ids, removing)
		}
	}
}

type evidenceHistory struct {
	recordReader
	attempts    []operationstore.Attempt
	resolutions map[int]operationstore.Attempt
}

func (h evidenceHistory) Block(context.Context, string, string) (operationstore.BlockRecord, error) {
	return operationstore.BlockRecord{Attempts: len(h.attempts)}, nil
}
func (h evidenceHistory) Attempt(_ context.Context, _ string, _ string, number int) (operationstore.Attempt, error) {
	return h.attempts[number-1], nil
}
func (h evidenceHistory) LastResolution(_ context.Context, _ string, _ string, number int) (operationstore.Attempt, bool, error) {
	attempt, found := h.resolutions[number]
	return attempt, found, nil
}
func TestPreviousEvidenceSkipsNullAndRunningRecords(t *testing.T) {
	t.Parallel()
	for _, empty := range []string{"", "null"} {
		history := evidenceHistory{attempts: []operationstore.Attempt{{Phase: "observed", Evidence: []byte(`{"receipt":"uid"}`)}, {Phase: "observed", Evidence: []byte(empty)}, {Phase: "running"}}, resolutions: map[int]operationstore.Attempt{1: {Phase: "running"}, 2: {Phase: "observed", Evidence: []byte(empty)}}}
		got, err := previousBlockEvidence(context.Background(), history, "operation", "block")
		if err != nil || string(got) != `{"receipt":"uid"}` {
			t.Fatalf("previous evidence with %q empty: %s %v", empty, got, err)
		}
		history.attempts = history.attempts[1:2]
		history.resolutions = nil
		got, err = previousBlockEvidence(context.Background(), history, "operation", "block")
		if err != nil || len(got) != 0 {
			t.Fatalf("empty history: %s %v", got, err)
		}
	}
}

type multiKindLendingResolver struct{ testResolver }

func (r multiKindLendingResolver) Resolve(kind, implementation string) (Capability, bool) {
	if kind == "CephConsumer" && implementation == r.Bindings()[0].Implementation {
		return r.capability, true
	}
	return r.testResolver.Resolve(kind, implementation)
}
func TestProducedInputsLendAcrossProducerKinds(t *testing.T) {
	t.Parallel()
	h, c := lendingHarness(t)
	h.capability.definitions[1].Kind = "CephConsumer"
	c.references["consumer"][1].Kind = "CephConsumer"
	h.service.capabilities = multiKindLendingResolver{testResolver{capability: c}}
	if _, err := apply(h); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(c.borrowed, []string{"producer-a-private-material", "producer-b-private-material"}) {
		t.Fatal(c.borrowed)
	}
}
