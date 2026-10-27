package lifecycle

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

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
	key := request.Block + "/" + request.Name
	value, found := b.produced[key]
	return secretstore.ProducedMaterial{Material: secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte(value)}), Unproved: b.unproved[key]}, found, nil
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
	h, c := lendingHarness(t)
	definitions := h.capability.definitions
	block := reconciliation.Block{BlockDefinition: definitions[2]}
	for _, tc := range []struct {
		name                           string
		found, unproved, removal, want bool
	}{{"missing", false, false, false, false}, {"unproved apply", true, true, false, false}, {"unproved removal", true, true, true, true}, {"proved", true, false, false, true}} {
		t.Run(tc.name, func(t *testing.T) {
			lend, release, err := h.service.producedLender(block, definitions, tc.removal, func(context.Context, string, string) (secretstore.ProducedMaterial, bool, error) {
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
