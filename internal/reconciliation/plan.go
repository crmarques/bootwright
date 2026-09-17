package reconciliation

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"strings"
	"unicode/utf8"
)

const (
	MaxBlocks       = 256
	MaxGroups       = 32
	MaxMachines     = 512
	MaxImpacts      = 64
	MaxExclusive    = 8
	MaxRequestBytes = 64 << 10
	maxDescription  = 200
	maxIdentifier   = 63
)

type Verb string

const (
	Apply   Verb = "apply"
	Destroy Verb = "destroy"
)

// Group is one presentation step a capability performs across one or more
// Machines. The plan freezes its identity and order before effects, so an
// adapter can neither invent nor reorder a group.
type Group struct {
	ID          string   `json:"id"`
	Description string   `json:"description"`
	Machines    []string `json:"machines"`
}

// ObjectRef names one API object whose realization a block waits for. A
// capability states its requirements in these terms, so it never learns
// another capability's block-identity grammar.
type ObjectRef struct {
	Kind   string
	Object string
}

// BlockDefinition is what a capability contributes. Reconciliation resolves its
// requirements into dependencies, orders the definitions and freezes them as
// blocks; a capability never chooses plan order.
type BlockDefinition struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	Stage       Stage  `json:"stage"`
	// Requires resolves into Dependencies before the plan freezes, so a frozen
	// block carries only the block identities it waits for.
	Requires     []ObjectRef `json:"-"`
	Dependencies []string    `json:"dependencies"`
	// Exclusive names host resources this block does not share while it runs.
	// Two blocks that name one key never run at the same time, even when the
	// graph would allow it, because the graph orders what one block needs from
	// another and not what two of them would write to at once.
	Exclusive      []string        `json:"exclusive,omitempty"`
	Impacts        []string        `json:"impacts"`
	Consumes       []string        `json:"consumes"`
	Groups         []Group         `json:"groups"`
	Kind           string          `json:"kind"`
	Object         string          `json:"object"`
	Implementation string          `json:"implementation"`
	ContentDigest  string          `json:"contentDigest"`
	Request        json.RawMessage `json:"request"`
}

type Block struct {
	BlockDefinition
	RequestDigest string `json:"requestDigest"`
}

type Plan struct {
	Verb   Verb    `json:"verb"`
	Blocks []Block `json:"blocks"`
}

// NewPlan orders definitions deterministically and freezes their request
// digests. Equal definitions in any input order produce an identical plan, so
// the digest identifies intent rather than iteration order.
func NewPlan(verb Verb, definitions []BlockDefinition) (Plan, error) {
	if verb != Apply && verb != Destroy {
		return Plan{}, planError("lifecycle plan verb is not recognized")
	}
	if len(definitions) > MaxBlocks {
		return Plan{}, planError("lifecycle plan exceeds its block limit")
	}
	resolved, err := resolveRequirements(definitions)
	if err != nil {
		return Plan{}, err
	}
	ordered, err := order(resolved)
	if err != nil {
		return Plan{}, err
	}
	blocks := make([]Block, 0, len(ordered))
	for _, definition := range ordered {
		digest, err := RequestDigest(definition)
		if err != nil {
			return Plan{}, err
		}
		blocks = append(blocks, Block{BlockDefinition: clone(definition), RequestDigest: digest})
	}
	return Plan{Verb: verb, Blocks: blocks}, nil
}

// Inverse plans removal from an apply: every edge turns around, so a block
// waits on its own dependents because what it provided stays in use until they
// are gone. Reversing the order alone would not do it, because the executor
// starts blocks whose dependencies are done and would take a provider first.
// The result is built through NewPlan so a removal is ordered by the one
// canonical rule every plan obeys and can be rebuilt from its own record.
func (p Plan) Inverse() (Plan, error) {
	dependents := map[string][]string{}
	for _, block := range p.Blocks {
		for _, dependency := range block.Dependencies {
			dependents[dependency] = append(dependents[dependency], block.ID)
		}
	}
	definitions := make([]BlockDefinition, 0, len(p.Blocks))
	for _, block := range p.Blocks {
		definition := clone(block.BlockDefinition)
		definition.Dependencies = dependents[block.ID]
		definitions = append(definitions, definition)
	}
	return NewPlan(Destroy, definitions)
}

// Retain narrows a plan to the blocks named and drops every edge to a block it
// does not keep, because a block outside the set is not this plan's to wait
// for. What remains is ordered by the same canonical rule, so a narrowed
// removal is still a plan this executable can rebuild from its frozen record.
func (p Plan) Retain(ids []string) (Plan, error) {
	definitions := make([]BlockDefinition, 0, len(p.Blocks))
	for _, block := range p.Blocks {
		if !slices.Contains(ids, block.ID) {
			continue
		}
		definition := clone(block.BlockDefinition)
		definition.Dependencies = slices.DeleteFunc(definition.Dependencies, func(id string) bool {
			return !slices.Contains(ids, id)
		})
		definitions = append(definitions, definition)
	}
	return NewPlan(p.Verb, definitions)
}

func (p Plan) Block(id string) (Block, bool) {
	for _, block := range p.Blocks {
		if block.ID == id {
			return cloneBlock(block), true
		}
	}
	return Block{}, false
}

func (p Plan) Digest() (string, error) {
	data, err := json.Marshal(struct {
		Domain string  `json:"domain"`
		Verb   Verb    `json:"verb"`
		Blocks []Block `json:"blocks"`
	}{"bootwright.reconciliation.plan-v1", p.Verb, p.Blocks})
	if err != nil {
		return "", planError("lifecycle plan cannot be canonically represented")
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}

func RequestDigest(definition BlockDefinition) (string, error) {
	if err := canonicalRequest(definition.Request); err != nil {
		return "", err
	}
	data, err := json.Marshal(struct {
		Domain         string          `json:"domain"`
		Kind           string          `json:"kind"`
		Object         string          `json:"object"`
		Implementation string          `json:"implementation"`
		ContentDigest  string          `json:"contentDigest"`
		Request        json.RawMessage `json:"request"`
	}{"bootwright.reconciliation.request-v1", definition.Kind, definition.Object, definition.Implementation, definition.ContentDigest, definition.Request})
	if err != nil {
		return "", planError("lifecycle block request cannot be canonically represented")
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}

// resolveRequirements turns each capability's API-object requirements into
// block dependencies. A block never depends on itself, and a requirement no
// block in this plan realizes refuses before the plan exists.
func resolveRequirements(definitions []BlockDefinition) ([]BlockDefinition, error) {
	realized := map[ObjectRef][]string{}
	for _, definition := range definitions {
		reference := ObjectRef{Kind: definition.Kind, Object: definition.Object}
		realized[reference] = append(realized[reference], definition.ID)
	}
	resolved := make([]BlockDefinition, 0, len(definitions))
	for _, definition := range definitions {
		dependencies := slices.Clone(definition.Dependencies)
		for _, requirement := range definition.Requires {
			blocks, found := realized[requirement]
			if !found {
				return nil, planError("lifecycle block requires " + requirement.Kind + "/" + requirement.Object + ", which no block in this plan realizes")
			}
			for _, id := range blocks {
				if id != definition.ID && !slices.Contains(dependencies, id) {
					dependencies = append(dependencies, id)
				}
			}
		}
		slices.Sort(dependencies)
		definition.Dependencies = slices.Compact(dependencies)
		definition.Requires = nil
		resolved = append(resolved, definition)
	}
	return resolved, nil
}

// order writes the plan wave by wave: every block sits after each block it
// waits for, and blocks that wait for nothing more than each other's depth sit
// together in identity order. The result reads as the schedule rather than as
// one arbitrary sequential walk of the same graph, and the numbered plan an
// operator confirms is the order the work is actually started in.
func order(definitions []BlockDefinition) ([]BlockDefinition, error) {
	known := make(map[string]BlockDefinition, len(definitions))
	for _, definition := range definitions {
		if err := validateDefinition(definition); err != nil {
			return nil, err
		}
		if _, duplicate := known[definition.ID]; duplicate {
			return nil, planError("lifecycle plan repeats a block identity")
		}
		known[definition.ID] = definition
	}
	wave, err := waves(known)
	if err != nil {
		return nil, err
	}
	ordered := make([]BlockDefinition, 0, len(definitions))
	for _, definition := range definitions {
		ordered = append(ordered, known[definition.ID])
	}
	slices.SortFunc(ordered, func(x, y BlockDefinition) int {
		if depth := wave[x.ID] - wave[y.ID]; depth != 0 {
			return depth
		}
		return strings.Compare(x.ID, y.ID)
	})
	return ordered, nil
}

// waves measures how deep each block sits in the graph: a block that waits for
// nothing is in the first wave, and every other block is one wave past the
// deepest block it waits for. A plan whose dependencies cannot all be measured
// contains a cycle.
func waves(known map[string]BlockDefinition) (map[string]int, error) {
	remaining := make(map[string]int, len(known))
	dependents := make(map[string][]string, len(known))
	depth := make(map[string]int, len(known))
	var ready []string
	for id, definition := range known {
		for _, dependency := range definition.Dependencies {
			if _, ok := known[dependency]; !ok {
				return nil, planError("lifecycle block depends on a block the plan does not contain")
			}
			dependents[dependency] = append(dependents[dependency], id)
		}
		remaining[id] = len(definition.Dependencies)
		if remaining[id] == 0 {
			ready = append(ready, id)
		}
	}
	measured := 0
	for len(ready) != 0 {
		id := ready[0]
		ready = ready[1:]
		measured++
		for _, dependent := range dependents[id] {
			depth[dependent] = max(depth[dependent], depth[id]+1)
			remaining[dependent]--
			if remaining[dependent] == 0 {
				ready = append(ready, dependent)
			}
		}
	}
	if measured != len(known) {
		return nil, planError("lifecycle plan dependencies form a cycle")
	}
	return depth, nil
}

// Schedule is what a frozen plan's shape says about running it: the wave each
// block can start in, how many waves there are, and how many blocks share the
// widest one. A wave is the earliest round a block can start, never a barrier:
// a later wave overlaps an earlier one as soon as its own dependencies settle.
type Schedule struct {
	Waves  map[string]int
	Count  int
	Widest int
}

// ScheduleOf measures a frozen plan. A plan this executable produced is
// already ordered by wave, so the answer restates its own shape rather than
// deriving a different one.
func ScheduleOf(plan Plan) Schedule {
	known := make(map[string]BlockDefinition, len(plan.Blocks))
	for _, block := range plan.Blocks {
		known[block.ID] = block.BlockDefinition
	}
	depth, err := waves(known)
	if err != nil {
		return Schedule{Waves: map[string]int{}}
	}
	population := map[int]int{}
	schedule := Schedule{Waves: depth}
	for _, wave := range depth {
		population[wave]++
		schedule.Count = max(schedule.Count, wave+1)
		schedule.Widest = max(schedule.Widest, population[wave])
	}
	return schedule
}

func validateDefinition(definition BlockDefinition) error {
	if !ValidSegment(definition.ID) {
		return planError("lifecycle block identity is not a safe segment")
	}
	if !safeDescription(definition.Description) {
		return planError("lifecycle block requires a safe single-line description")
	}
	if !ValidStage(definition.Stage) {
		return planError("lifecycle block requires a recognized stage")
	}
	if definition.Kind == "" || definition.Object == "" || definition.Implementation == "" {
		return planError("lifecycle block requires its kind, object and implementation identity")
	}
	if !validDigest(definition.ContentDigest) {
		return planError("lifecycle block requires its implementation content digest")
	}
	if len(definition.Dependencies) > MaxBlocks || len(definition.Impacts) > MaxImpacts || len(definition.Groups) > MaxGroups {
		return planError("lifecycle block exceeds its collection limits")
	}
	if len(definition.Exclusive) > MaxExclusive || !uniqueSorted(definition.Exclusive) {
		return planError("lifecycle block exclusive resources must be bounded, unique and ordered")
	}
	for _, key := range definition.Exclusive {
		if !safeDescription(key) {
			return planError("lifecycle block exclusive resources must be safe single-line text")
		}
	}
	if slices.Contains(definition.Dependencies, definition.ID) {
		return planError("lifecycle block cannot depend on itself")
	}
	if !uniqueSorted(definition.Dependencies) {
		return planError("lifecycle block dependencies must be unique and ordered")
	}
	if !uniqueSorted(definition.Consumes) {
		return planError("lifecycle block authorizations must be unique and ordered")
	}
	for _, token := range definition.Consumes {
		if !ValidAuthorization(token) {
			return planError("lifecycle block consumes an unrecognized authorization")
		}
	}
	for _, impact := range definition.Impacts {
		if !safeDescription(impact) {
			return planError("lifecycle block impacts must be safe single-line text")
		}
	}
	groups := make([]string, 0, len(definition.Groups))
	for _, group := range definition.Groups {
		if !ValidSegment(group.ID) || !safeDescription(group.Description) {
			return planError("lifecycle group requires a safe identity and description")
		}
		if len(group.Machines) == 0 || len(group.Machines) > MaxMachines || !uniqueSorted(group.Machines) {
			return planError("lifecycle group requires a bounded unique ordered Machine set")
		}
		groups = append(groups, group.ID)
	}
	if !unique(groups) {
		return planError("lifecycle group identities must be unique within their block")
	}
	return canonicalRequest(definition.Request)
}

// canonicalRequest rejects anything a later reader could decode differently:
// the digest is only an identity if the bytes have exactly one meaning.
func canonicalRequest(request json.RawMessage) error {
	if len(request) == 0 || len(request) > MaxRequestBytes || request[0] != '{' {
		return planError("lifecycle block request must be a bounded JSON object")
	}
	decoder := json.NewDecoder(bytes.NewReader(request))
	decoder.UseNumber()
	var value map[string]any
	if err := decoder.Decode(&value); err != nil {
		return planError("lifecycle block request is malformed")
	}
	if decoder.More() {
		return planError("lifecycle block request contains trailing data")
	}
	canonical, err := json.Marshal(value)
	if err != nil || !bytes.Equal(request, canonical) {
		return planError("lifecycle block request is not canonical")
	}
	return nil
}

func ValidSegment(value string) bool {
	if len(value) == 0 || len(value) > maxIdentifier {
		return false
	}
	for index, c := range value {
		alphanumeric := c >= 'a' && c <= 'z' || c >= '0' && c <= '9'
		if !alphanumeric && !(c == '-' && index != 0 && index != len(value)-1) {
			return false
		}
	}
	return true
}

func safeDescription(value string) bool {
	if value == "" || len(value) > maxDescription || !utf8.ValidString(value) {
		return false
	}
	return !strings.ContainsFunc(value, func(c rune) bool {
		return c < 0x20 || c == 0x7f
	})
}

func validDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && hex.EncodeToString(decoded) == value
}

func unique(values []string) bool {
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		if seen[value] {
			return false
		}
		seen[value] = true
	}
	return true
}

func uniqueSorted(values []string) bool {
	return slices.IsSorted(values) && unique(values)
}

func clone(definition BlockDefinition) BlockDefinition {
	definition.Requires = nil
	definition.Dependencies = slices.Clone(definition.Dependencies)
	definition.Exclusive = slices.Clone(definition.Exclusive)
	definition.Impacts = slices.Clone(definition.Impacts)
	definition.Consumes = slices.Clone(definition.Consumes)
	definition.Groups = slices.Clone(definition.Groups)
	for index := range definition.Groups {
		definition.Groups[index].Machines = slices.Clone(definition.Groups[index].Machines)
	}
	definition.Request = slices.Clone(definition.Request)
	return definition
}

func cloneBlock(block Block) Block {
	block.BlockDefinition = clone(block.BlockDefinition)
	return block
}
