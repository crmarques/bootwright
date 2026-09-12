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

// BlockDefinition is what a capability contributes. Reconciliation orders the
// definitions and freezes them as blocks; a capability never chooses plan order.
type BlockDefinition struct {
	ID             string          `json:"id"`
	Description    string          `json:"description"`
	Dependencies   []string        `json:"dependencies"`
	Impacts        []string        `json:"impacts"`
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
	ordered, err := order(definitions)
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

// Inverse plans removal from a completed apply: dependents are removed before
// the dependencies they needed, so the order is exactly reversed.
func (p Plan) Inverse() Plan {
	blocks := make([]Block, 0, len(p.Blocks))
	for index := len(p.Blocks) - 1; index >= 0; index-- {
		blocks = append(blocks, cloneBlock(p.Blocks[index]))
	}
	return Plan{Verb: Destroy, Blocks: blocks}
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
	remaining := make(map[string]int, len(definitions))
	dependents := make(map[string][]string, len(definitions))
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
	slices.Sort(ready)
	ordered := make([]BlockDefinition, 0, len(definitions))
	for len(ready) != 0 {
		id := ready[0]
		ready = ready[1:]
		ordered = append(ordered, known[id])
		released := dependents[id]
		slices.Sort(released)
		for _, dependent := range released {
			remaining[dependent]--
			if remaining[dependent] == 0 {
				ready = append(ready, dependent)
			}
		}
		slices.Sort(ready)
	}
	if len(ordered) != len(definitions) {
		return nil, planError("lifecycle plan dependencies form a cycle")
	}
	return ordered, nil
}

func validateDefinition(definition BlockDefinition) error {
	if !ValidSegment(definition.ID) {
		return planError("lifecycle block identity is not a safe segment")
	}
	if !safeDescription(definition.Description) {
		return planError("lifecycle block requires a safe single-line description")
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
	if slices.Contains(definition.Dependencies, definition.ID) {
		return planError("lifecycle block cannot depend on itself")
	}
	if !uniqueSorted(definition.Dependencies) {
		return planError("lifecycle block dependencies must be unique and ordered")
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
	definition.Dependencies = slices.Clone(definition.Dependencies)
	definition.Impacts = slices.Clone(definition.Impacts)
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
