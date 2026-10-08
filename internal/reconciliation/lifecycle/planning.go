package lifecycle

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"maps"
	"slices"
	"strconv"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

// capabilityBinding is what the resolved capabilities together need before
// their blocks may run: the host resources they claim and the Secrets their
// execution consumes.
type capabilityBinding struct {
	reservations []prerequisites.HostReservation
	secrets      []string
	// controller is the Machine this Environment selects. A first apply binds
	// the context to the host it runs on under that name.
	controller string
}

// compile reads the frozen input once, so a decision never compiles the same
// revision twice to answer two questions about it.
func (s Service) compile(ctx context.Context, view View) (*compilation.State, error) {
	state, report, err := s.compiler.Compile(ctx, view.Inputs())
	if err != nil {
		return nil, err
	}
	if state == nil || report == nil {
		return nil, failure("lifecycle.state", "the frozen desired state could not be compiled", "repair the input with validate and import it again")
	}
	return state, nil
}

// dependOnController makes every other block wait for the controller block,
// because the clients that block installs are what their adapters run. This is
// the one dependency the engine owns; a plan without a controller block is
// ordered by its capabilities alone.
func dependOnController(definitions []reconciliation.BlockDefinition) []reconciliation.BlockDefinition {
	prerequisite := ""
	for _, definition := range definitions {
		if definition.Stage == reconciliation.StageController {
			prerequisite = definition.ID
			break
		}
	}
	if prerequisite == "" {
		return definitions
	}
	for index, definition := range definitions {
		if definition.ID == prerequisite || slices.Contains(definition.Dependencies, prerequisite) {
			continue
		}
		dependencies := append(slices.Clone(definition.Dependencies), prerequisite)
		slices.Sort(dependencies)
		definitions[index].Dependencies = dependencies
	}
	return definitions
}

// planFrom asks every resolvable capability for its blocks. Planning is pure:
// it opens no payload, contacts nothing and materializes no Secret.
func (s Service) planFrom(ctx context.Context, view View, state *compilation.State, verb reconciliation.Verb) (reconciliation.Plan, capabilityBinding, error) {
	controller, err := controllerMachine(state)
	if err != nil {
		return reconciliation.Plan{}, capabilityBinding{}, err
	}
	bindings := s.capabilities.Bindings()
	if len(bindings) == 0 {
		return reconciliation.Plan{}, capabilityBinding{}, failure("lifecycle.state", "no lifecycle capability is available for the selected state", "")
	}
	input := PlanInput{Verb: verb, Context: view.Identity(), State: state, Controller: controller}
	var definitions []reconciliation.BlockDefinition
	var placed []SSHReservation
	binding := capabilityBinding{controller: controller}
	for _, bound := range bindings {
		capability, ok := s.capabilities.Resolve(bound.Kind, bound.Implementation)
		if !ok {
			return reconciliation.Plan{}, capabilityBinding{}, failure("lifecycle.state", "a declared lifecycle capability does not resolve", "")
		}
		contribution, err := capability.Plan(ctx, input)
		if err != nil {
			return reconciliation.Plan{}, capabilityBinding{}, err
		}
		definitions = append(definitions, contribution.Definitions...)
		binding.reservations = append(binding.reservations, contribution.Reservations...)
		placed = append(placed, contribution.SSHReservations...)
		for _, reference := range contribution.Secrets {
			if !slices.Contains(binding.secrets, reference) {
				binding.secrets = append(binding.secrets, reference)
			}
		}
	}
	slices.Sort(binding.secrets)
	if err := RefuseTemplateDelimiters(view.Identity().Name, definitions); err != nil {
		return reconciliation.Plan{}, capabilityBinding{}, err
	}
	if err := refuseOwnSocketConflicts(state.Effective(), controller, binding.reservations, placed); err != nil {
		return reconciliation.Plan{}, capabilityBinding{}, err
	}
	definitions = dependOnController(definitions)
	plan, err := reconciliation.NewPlan(reconciliation.Apply, definitions)
	if err != nil {
		return reconciliation.Plan{}, capabilityBinding{}, err
	}
	if verb == reconciliation.Destroy {
		if plan, err = plan.Inverse(); err != nil {
			return reconciliation.Plan{}, capabilityBinding{}, err
		}
	}
	return plan, binding, nil
}

// refuseOwnSocketConflicts compares the sockets this context's own blocks claim
// on one host as another context's are compared on the controller, so two of
// them that could never both listen refuse before the plan exists rather than
// when the second one starts. A claim is qualified by the host its block is
// placed on: the controller, or the host its SSH placement reaches, named by
// address and port rather than by Machine, so two Machines reaching one host
// share it and one reaching the controller shares the controller's. Claims on
// two hosts never conflict. The controller is compared first, then each SSH
// host in the name order of the first Machine reaching it.
func refuseOwnSocketConflicts(catalog api.Catalog, controller string, reservations []prerequisites.HostReservation, placed []SSHReservation) error {
	own, _ := catalog.Find(api.Machine, controller)
	local := &hostClaims{claims: slices.Clone(reservations), through: make([]string, len(reservations))}
	remote := map[string]*hostClaims{}
	for _, claim := range placed {
		host := local
		if !machine.ReachesController(own, claim.Address, claim.Port) {
			endpoint := machine.SSHHost(claim.Address, claim.Port)
			if host = remote[endpoint]; host == nil {
				host = &hostClaims{endpoint: endpoint}
				remote[endpoint] = host
			}
		}
		host.claims = append(host.claims, claim.Reservation)
		host.through = append(host.through, claim.Machine)
	}
	ordered := slices.SortedFunc(maps.Values(remote), func(first, second *hostClaims) int {
		return strings.Compare(slices.Min(first.through), slices.Min(second.through))
	})
	for _, host := range append([]*hostClaims{local}, ordered...) {
		if conflict, found := prerequisites.ConflictingSockets(host.claims); found {
			return socketConflict(conflict, host.name(conflict))
		}
	}
	return nil
}

// hostClaims is every socket claim this context places on one host: through
// names the Machine each claim reaches it by over SSH, empty for a claim placed
// on the controller, and endpoint is where an SSH host is reached.
type hostClaims struct {
	endpoint string
	claims   []prerequisites.HostReservation
	through  []string
}

// name is how a refusal names the host two conflicting claims share: the
// controller, or an SSH host by its Machine, and each Machine through which
// either claim reaches that host over SSH when it is not the host's own name,
// so an operator sees why two Machines' services meet. A claim is found by the
// kind and service the refusal names it by.
func (h *hostClaims) name(conflict prerequisites.SocketConflict) string {
	var machines []string
	for _, claim := range []prerequisites.HostReservation{conflict.First, conflict.Second} {
		index := slices.IndexFunc(h.claims, func(held prerequisites.HostReservation) bool {
			return held.Kind == claim.Kind && held.Service == claim.Service
		})
		name := string(api.Machine) + "/" + h.through[index]
		if h.through[index] != "" && !slices.Contains(machines, name) {
			machines = append(machines, name)
		}
	}
	slices.Sort(machines)
	switch {
	case h.endpoint == "" && len(machines) == 0:
		return "the controller"
	case h.endpoint == "" && len(machines) == 1:
		return "the controller, which " + machines[0] + " reaches over SSH"
	case h.endpoint == "":
		return "the controller, which " + strings.Join(machines, " and ") + " both reach over SSH"
	case len(machines) == 1:
		return "the SSH host " + machines[0]
	}
	return "the SSH host at " + h.endpoint + ", which " + strings.Join(machines, " and ") + " both reach"
}

func socketConflict(conflict prerequisites.SocketConflict, host string) error {
	return failure("api.invariant",
		"this context's "+conflict.First.Kind+" "+conflict.First.Service+" at "+conflict.FirstSocket+" and "+
			conflict.Second.Kind+" "+conflict.Second.Service+" at "+conflict.SecondSocket+" cannot both listen on "+host,
		"give one of them another bind address or port")
}

// templateDelimiters open a Jinja expression, statement and comment. No request
// Bootwright plans carries one of its own, so one there came from a value.
var templateDelimiters = []string{"{{", "{%", "{#"}

// ansibleReservedKeys are the keys ansible-core decodes an object holding one
// of as a typed value, so no runner can hand a request keyed by one as data.
// They are the keys ansible.ExtraVariables refuses.
var ansibleReservedKeys = []string{"__ansible_type", "__ansible_unsafe", "__ansible_vault"}

// RefuseTemplateDelimiters refuses every block whose request holds a template
// delimiter in a string or a mapping key, or a key ansible-core reserves,
// whatever its stage, with one diagnostic per block naming the first such
// field and how many more hold one. A runner already hands every string to
// Ansible as data; this keeps a value that would be code there, or that no
// runner could encode, out of a plan at all.
func RefuseTemplateDelimiters(contextName string, definitions []reconciliation.BlockDefinition) error {
	return refuseTemplateValues(contextName, contextCommand(contextName, "plan"), definitions)
}

// RefuseBoundedTemplateDelimiters refuses a bounded run's requests as
// RefuseTemplateDelimiters refuses a plan's. A bounded run is never planned,
// so its remedy ends with retry, the command that carries the run again.
func RefuseBoundedTemplateDelimiters(contextName, retry string, definitions []reconciliation.BlockDefinition) error {
	return refuseTemplateValues(contextName, retry, definitions)
}

// refuseTemplateValues is the refusal of both, whose remedy ends with retry.
func refuseTemplateValues(contextName, retry string, definitions []reconciliation.BlockDefinition) error {
	var refused []diagnostics.Diagnostic
	for _, definition := range definitions {
		hits := templateHits(definition.Request)
		if len(hits) == 0 {
			continue
		}
		object := definition.Kind + "/" + definition.Object
		message := "the request planned for " + object + " holds " + hits[0].what + " in " + hits[0].where
		if more := len(hits) - 1; more > 0 {
			message += ", and " + strconv.Itoa(more) + " more of its fields hold one"
		}
		var removed []string
		if slices.ContainsFunc(hits, func(hit templateHit) bool { return !hit.reserved }) {
			removed = append(removed, `"{{", "{%" and "{#"`)
		}
		if slices.ContainsFunc(hits, func(hit templateHit) bool { return hit.reserved }) {
			removed = append(removed, `every key named "__ansible_type", "__ansible_unsafe" or "__ansible_vault"`)
		}
		// A plan reads the context's imported input, so the repaired values are
		// imported before the context is planned again.
		remedy := "remove " + strings.Join(removed, " and ") + " from the desired-state values " + object + " is planned from, " +
			"import them with bootwright context update --name " + contextName + " --input-dir <dir>, then run " + retry
		refused = append(refused, diagnostics.Diagnostic{
			Severity: "error", Code: "api.value", Message: message, Remediation: remedy,
			Object: &diagnostics.ObjectIdentity{APIVersion: api.APIVersion, Kind: definition.Kind, Name: definition.Object},
		})
	}
	if len(refused) == 0 {
		return nil
	}
	slices.SortFunc(refused, func(first, second diagnostics.Diagnostic) int {
		return cmp.Or(strings.Compare(first.Object.Kind, second.Object.Kind), strings.Compare(first.Object.Name, second.Object.Name), strings.Compare(first.Message, second.Message))
	})
	return &diagnostics.Failure{Diagnostics: refused}
}

// templateHit is one field of a request holding a delimiter or a reserved key:
// what it holds, where the field is, and whether that is a reserved key.
type templateHit struct {
	what, where string
	reserved    bool
}

// templateHits reads a request depth-first, keys in sorted order and items in
// index order. A request that does not decode holds none here, because the
// plan refuses its shape itself.
func templateHits(request []byte) []templateHit {
	decoder := json.NewDecoder(bytes.NewReader(request))
	decoder.UseNumber()
	var decoded any
	if decoder.Decode(&decoded) != nil {
		return nil
	}
	var hits []templateHit
	collectTemplateHits(decoded, "", &hits)
	return hits
}

func collectTemplateHits(value any, path string, hits *[]templateHit) {
	switch typed := value.(type) {
	case string:
		if marker, index := firstDelimiter(typed); marker != "" {
			where := cmp.Or(path, "the request")
			if strings.Contains(typed, "\n") {
				where = "line " + strconv.Itoa(strings.Count(typed[:index], "\n")+1) + " of " + where
			}
			*hits = append(*hits, templateHit{what: delimiterHeld(marker), where: where})
		}
	case []any:
		for index, item := range typed {
			collectTemplateHits(item, path+"["+strconv.Itoa(index)+"]", hits)
		}
	case map[string]any:
		for _, key := range slices.Sorted(maps.Keys(typed)) {
			if marker, _ := firstDelimiter(key); marker != "" {
				*hits = append(*hits, templateHit{what: delimiterHeld(marker), where: "a key of " + cmp.Or(path, "the request")})
			} else if slices.Contains(ansibleReservedKeys, key) {
				*hits = append(*hits, templateHit{what: "the ansible-core reserved key " + strconv.Quote(key), where: cmp.Or(path, "the request"), reserved: true})
			}
			collectTemplateHits(typed[key], fieldPath(path, key), hits)
		}
	}
}

// fieldPath names a member as a dotted path does, quoting a key that is not
// letters, digits, underscores and hyphens.
func fieldPath(path, key string) string {
	plain := key != "" && strings.Trim(key, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_-") == ""
	switch {
	case !plain:
		return path + "[" + strconv.Quote(key) + "]"
	case path == "":
		return key
	}
	return path + "." + key
}

func delimiterHeld(marker string) string {
	return "the template delimiter " + strconv.Quote(marker)
}

func firstDelimiter(text string) (string, int) {
	marker, first := "", -1
	for _, delimiter := range templateDelimiters {
		if index := strings.Index(text, delimiter); index >= 0 && (first < 0 || index < first) {
			marker, first = delimiter, index
		}
	}
	return marker, first
}

// refuseUnsupported refuses every selected object whose realization this
// executable cannot perform, before any registration or effect, with one
// diagnostic per object and reason that names the object, the reason and the
// remedy. A capability reports what it cannot do within its own kinds; the
// engine reports every effect-bearing kind no capability claims at all.
func (s Service) refuseUnsupported(state *compilation.State) error {
	refused := s.unsupportedRefusals(state)
	if len(refused) == 0 {
		return nil
	}
	reported := make([]diagnostics.Diagnostic, 0, len(refused))
	for _, refusal := range refused {
		remediation := refusal.Remediation
		if remediation == "" {
			remediation = "correct " + refusal.identity()
		}
		reported = append(reported, diagnostics.Diagnostic{
			Severity: "error", Code: "lifecycle.unsupported", Message: refusal.Reason, Remediation: remediation,
			Object: &diagnostics.ObjectIdentity{APIVersion: api.APIVersion, Kind: refusal.Kind, Name: refusal.Name},
		})
	}
	return &diagnostics.Failure{Diagnostics: reported}
}

// unsupportedRefusals is every refusal of a selected object this executable
// cannot realize, in canonical order: what each bound capability reports
// within its own kinds, and every effect-bearing object no capability claims.
// It is pure, so status reads the same list a plan refuses.
func (s Service) unsupportedRefusals(state *compilation.State) []Refusal {
	var refused []Refusal
	for _, bound := range s.capabilities.Bindings() {
		capability, ok := s.capabilities.Resolve(bound.Kind, bound.Implementation)
		if !ok {
			continue
		}
		if reporter, ok := capability.(UnsupportedReporter); ok {
			refused = append(refused, reporter.Unsupported(state)...)
		}
	}
	return SortRefusals(append(refused, Unrealizable(state.Effective(), s.capabilityKinds())...))
}

const supportedExample = "examples/lab-rhel"

// capabilityKinds is the distinct API-kind set this build binds at least one
// implementation for, in binding order.
func (s Service) capabilityKinds() []string {
	var kinds []string
	for _, bound := range s.capabilities.Bindings() {
		if !slices.Contains(kinds, bound.Kind) {
			kinds = append(kinds, bound.Kind)
		}
	}
	return kinds
}

func controllerMachine(state *compilation.State) (string, error) {
	return ControllerMachine(state.Effective())
}

// ControllerMachine names the Machine this context's controller runs on. Every
// placement decision starts from it: the controller is local, and every other
// host is reached through the SSH access its Machine authors.
func ControllerMachine(catalog api.Catalog) (string, error) {
	environments := catalog.OfKind(api.Environment)
	if len(environments) != 1 {
		return "", failure("lifecycle.state", "the selected state does not contain exactly one Environment", "repair the input with validate")
	}
	name := environments[0].Spec().Get("controller", "machineRef").Text()
	if name == "" {
		return "", failure("lifecycle.state", "the Environment declares no controller Machine", "set spec.controller.machineRef")
	}
	return name, nil
}
