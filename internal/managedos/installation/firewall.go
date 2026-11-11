package installation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"strings"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/substrate"
)

const (
	firewallOwned  = "owned"
	firewallAbsent = "absent"
)

type FirewallMembership struct {
	Interface string `json:"interface"`
	Zone      string `json:"zone"`
}

type FirewallServiceEvidence struct {
	Covered     bool                 `json:"covered"`
	Digest      string               `json:"digest"`
	Memberships []FirewallMembership `json:"memberships"`
	Name        string               `json:"name"`
	State       string               `json:"state"`
}

type FirewallEvidence struct {
	Absent        bool                      `json:"absent"`
	Postcondition bool                      `json:"postcondition"`
	Ready         bool                      `json:"ready"`
	Request       string                    `json:"request"`
	Retired       bool                      `json:"retired,omitempty"`
	Services      []FirewallServiceEvidence `json:"services"`
	Version       string                    `json:"version"`
}

type FirewallCapability struct {
	runner Runner
	ports  HostPortRequirements
}

func NewFirewall(runner Runner) FirewallCapability { return FirewallCapability{runner: runner} }

func (c FirewallCapability) WithHostPorts(requirements HostPortRequirements) FirewallCapability {
	c.ports = requirements
	return c
}

func (c FirewallCapability) Plan(ctx context.Context, input lifecycle.PlanInput) (lifecycle.CapabilityPlan, error) {
	plan := lifecycle.CapabilityPlan{Definitions: []reconciliation.BlockDefinition{}}
	if err := ctx.Err(); err != nil {
		return plan, err
	}
	if input.State == nil {
		return plan, refusal("lifecycle.state", "host firewall planning requires compiled state", "")
	}
	if c.ports == nil {
		return plan, nil
	}
	catalog := input.State.Effective()
	requirements, err := c.ports(catalog)
	if err != nil {
		return plan, err
	}
	requests, err := FirewallRequests(catalog, input.Controller, input.Context.Name, requirements)
	if err != nil {
		return plan, err
	}
	for _, request := range requests {
		canonical, err := request.Canonical()
		if err != nil {
			return plan, err
		}
		stage := reconciliation.StageMachines
		requires := []reconciliation.ObjectRef{{Kind: Kind, Object: request.Identity.Object, Family: InstallFamily}}
		var dependencies []string
		if request.Direct {
			stage, requires = reconciliation.StageInfrastructureServices, nil
			if !request.Preparation {
				dependencies = []string{FirewallPreparationBlockID(request.Identity.Object)}
				requires = FirewallProviderRequirements(catalog, request)
			}
		}
		plan.Definitions = append(plan.Definitions, reconciliation.BlockDefinition{
			ID: request.Identity.Block, Description: firewallDescription(input.Verb, request), Stage: stage,
			Requires: requires, Dependencies: dependencies,
			Family: FirewallFamily, Kind: Kind, Object: request.Identity.Object, Implementation: FirewallImplementation, ContentDigest: FirewallContentDigest(),
			Request: canonical, Impacts: firewallImpacts(input.Verb, request),
		})
		plan.Secrets = append(plan.Secrets, request.SecretReferences()...)
	}
	slices.Sort(plan.Secrets)
	plan.Secrets = slices.Compact(plan.Secrets)
	return plan, nil
}

func firewallDescription(verb reconciliation.Verb, request FirewallRequest) string {
	if request.Preparation {
		if verb == reconciliation.Destroy {
			return "retain the shared host firewall daemon of " + request.Identity.Object
		}
		return "prepare the shared host firewall daemon of " + request.Identity.Object
	}
	if verb == reconciliation.Destroy {
		return "remove the host firewall services of " + request.Identity.Object
	}
	return "admit the host port requirements of " + request.Identity.Object + " in its firewall"
}

func firewallImpacts(verb reconciliation.Verb, request FirewallRequest) []string {
	if request.Preparation && verb == reconciliation.Destroy {
		return nil
	}
	action := "configure-host-firewall "
	if verb == reconciliation.Destroy {
		action = "remove-host-firewall "
	}
	return []string{action + request.Identity.Object}
}

func (c FirewallCapability) Claimed(catalog api.Catalog) []string {
	if c.ports == nil {
		return nil
	}
	requirements, err := c.ports(catalog)
	if err != nil {
		return nil
	}
	var claimed []string
	for _, object := range catalog.OfKind(api.Machine) {
		if RealizesHostPorts(catalog, object.Name()) && (object.Spec().Get("os", "provided").Bool() && object.Spec().Get("firewall", "management").Text() == "managed" || slices.ContainsFunc(requirements, func(r HostPortRequirement) bool { return r.Machine == object.Name() })) {
			claimed = append(claimed, object.Identity())
		}
	}
	slices.Sort(claimed)
	return slices.Compact(claimed)
}

func (FirewallCapability) Removal(ctx context.Context, block reconciliation.Block) (lifecycle.Removal, error) {
	if err := ctx.Err(); err != nil {
		return lifecycle.Removal{}, err
	}
	request, err := DecodeFirewallRequest(block.Request)
	if err != nil {
		return lifecycle.Removal{}, err
	}
	return lifecycle.Removal{Description: firewallDescription(reconciliation.Destroy, request), Impacts: firewallImpacts(reconciliation.Destroy, request)}, nil
}

func (FirewallCapability) SecretReferences(block reconciliation.Block) ([]string, error) {
	request, err := DecodeFirewallRequest(block.Request)
	if err != nil {
		return nil, err
	}
	return request.SecretReferences(), nil
}

func (FirewallCapability) SecretClasses(block reconciliation.Block) (secrets.Classes, error) {
	request, err := DecodeFirewallRequest(block.Request)
	if err != nil {
		return nil, err
	}
	return request.Placement.SecretClasses().Add(secrets.EffectMaterial, request.FleetKeyRef), nil
}

func (FirewallCapability) RequiresOnlineRemoval(block reconciliation.Block) bool {
	request, err := DecodeFirewallRequest(block.Request)
	return err != nil || !request.Preparation
}

func (FirewallCapability) HostMachines(block reconciliation.Block) ([]string, error) {
	request, err := DecodeFirewallRequest(block.Request)
	if err != nil {
		return nil, err
	}
	return []string{request.Identity.Object}, nil
}

func (FirewallCapability) Quiescent(ctx context.Context, probe lifecycle.Probe) (lifecycle.Quiescence, error) {
	if err := ctx.Err(); err != nil {
		return lifecycle.Quiescence{State: lifecycle.Unproved}, err
	}
	if probe.RetiredWithHosts {
		return lifecycle.Quiescence{State: lifecycle.Quiescent, Reason: "its services leave with the Machine this removal deletes"}, nil
	}
	return lifecycle.Quiescence{State: lifecycle.Quiescent, Reason: "every block that published a requirement is removed before it"}, nil
}

func (c FirewallCapability) Apply(ctx context.Context, execution lifecycle.Execution) (lifecycle.Result, error) {
	return c.mutate(ctx, execution, "apply")
}

func (c FirewallCapability) Destroy(ctx context.Context, execution lifecycle.Execution) (lifecycle.Result, error) {
	if err := ctx.Err(); err != nil {
		return lifecycle.Result{Outcome: reconciliation.OutcomeCanceled}, err
	}
	if evidence, retained, err := retainedFirewall(execution); retained || err != nil {
		if err != nil {
			return lifecycle.Result{Outcome: reconciliation.OutcomeFailed}, err
		}
		return lifecycle.Result{Outcome: reconciliation.OutcomeUnchanged, Evidence: evidence}, nil
	}
	if execution.RetiredWithHosts {
		evidence, err := retiredFirewall(execution)
		if err != nil {
			return lifecycle.Result{Outcome: reconciliation.OutcomeFailed}, err
		}
		return lifecycle.Result{Outcome: reconciliation.OutcomeChanged, Evidence: evidence}, nil
	}
	return c.mutate(ctx, execution, "destroy")
}

func (c FirewallCapability) Observe(ctx context.Context, execution lifecycle.Execution) (lifecycle.Observation, error) {
	return c.observe(ctx, execution, false)
}

func (c FirewallCapability) ObserveRemoval(ctx context.Context, execution lifecycle.Execution) (lifecycle.Observation, error) {
	if err := ctx.Err(); err != nil {
		return lifecycle.Observation{Effect: reconciliation.EffectUnknown}, err
	}
	if evidence, retained, err := retainedFirewall(execution); retained || err != nil {
		if err != nil {
			return lifecycle.Observation{Effect: reconciliation.EffectUnknown}, err
		}
		return lifecycle.Observation{Effect: reconciliation.EffectCompleted, Evidence: evidence}, nil
	}
	if execution.RetiredWithHosts {
		evidence, err := retiredFirewall(execution)
		if err != nil {
			return lifecycle.Observation{Effect: reconciliation.EffectUnknown}, err
		}
		return lifecycle.Observation{Effect: reconciliation.EffectCompleted, Evidence: evidence}, nil
	}
	return c.observe(ctx, execution, true)
}

func retainedFirewall(execution lifecycle.Execution) (json.RawMessage, bool, error) {
	request, err := DecodeFirewallRequest(execution.Block.Request)
	if err != nil || !request.Preparation {
		return nil, false, err
	}
	evidence, err := json.Marshal(FirewallEvidence{Absent: true, Postcondition: true, Request: execution.Block.RequestDigest, Services: []FirewallServiceEvidence{}, Version: FirewallEvidenceVersion})
	return evidence, true, err
}

func retiredFirewall(execution lifecycle.Execution) (json.RawMessage, error) {
	request, err := DecodeFirewallRequest(execution.Block.Request)
	if err != nil {
		return nil, err
	}
	services := make([]FirewallServiceEvidence, 0, len(request.Services))
	for _, service := range request.Services {
		services = append(services, FirewallServiceEvidence{Name: service.Name, State: firewallAbsent, Memberships: []FirewallMembership{}})
	}
	return json.Marshal(FirewallEvidence{Ready: true, Absent: true, Postcondition: true, Request: execution.Block.RequestDigest, Retired: true, Services: services, Version: FirewallEvidenceVersion})
}

func (c FirewallCapability) mutate(ctx context.Context, execution lifecycle.Execution, operation string) (lifecycle.Result, error) {
	removal := operation == "destroy"
	result, request, digests, err := c.run(ctx, execution, operation, removal)
	if err != nil {
		return lifecycle.Result{Outcome: lifecycle.AttemptOutcome(err)}, err
	}
	outcome := map[string]reconciliation.Outcome{"changed": reconciliation.OutcomeChanged, "unchanged": reconciliation.OutcomeUnchanged}[result.Outcome]
	evidence, err := decodeFirewallEvidence(result.Evidence, execution.Block.RequestDigest)
	if outcome == "" || err != nil || evidence.Absent != removal || !evidence.Postcondition || firewallEffect(evidence, request, digests, removal) != reconciliation.EffectCompleted {
		return lifecycle.Result{Outcome: reconciliation.OutcomeUnknown}, refusal("lifecycle.state", "the host firewall adapter did not prove its postcondition on Machine/"+request.Identity.Object, "")
	}
	return lifecycle.Result{Outcome: outcome, Evidence: result.Evidence}, nil
}

func (c FirewallCapability) observe(ctx context.Context, execution lifecycle.Execution, removal bool) (lifecycle.Observation, error) {
	observation := lifecycle.Observation{Effect: reconciliation.EffectUnknown}
	result, request, digests, err := c.run(ctx, execution, "observe", removal)
	if err != nil {
		return observation, err
	}
	observation.Evidence = result.Evidence
	evidence, err := decodeFirewallEvidence(result.Evidence, execution.Block.RequestDigest)
	if err != nil {
		return observation, nil
	}
	observation.Effect = firewallEffect(evidence, request, digests, removal)
	return observation, nil
}

func (c FirewallCapability) run(ctx context.Context, execution lifecycle.Execution, operation string, removal bool) (lifecycle.RunResult, FirewallRequest, map[string]string, error) {
	if err := ctx.Err(); err != nil {
		return lifecycle.RunResult{}, FirewallRequest{}, nil, err
	}
	request, err := DecodeFirewallRequest(execution.Block.Request)
	if err != nil {
		return lifecycle.RunResult{}, request, nil, err
	}
	if c.runner == nil {
		return lifecycle.RunResult{}, request, nil, refusal("lifecycle.state", "the host firewall adapter is not configured", "")
	}
	if execution.ApplyOperation == "" {
		return lifecycle.RunResult{}, request, nil, refusal("lifecycle.state", "the host firewall of Machine/"+request.Identity.Object+" has no original apply operation", "")
	}
	values := map[string]string{}
	var materials []lifecycle.MaterialFile
	if !request.Direct {
		proofs := execution.ApplyProved
		if len(proofs) == 0 {
			proofs = execution.Proved
		}
		key, err := ProvedHostKey(proofs, request.Identity.Object, request.Address)
		if err != nil {
			return lifecycle.RunResult{}, request, nil, err
		}
		values["hostKey"] = key.Type + " " + key.PublicKey
		materials = []lifecycle.MaterialFile{{Name: "fleet-id", Part: secrets.PrivateKeyPart, Secret: request.FleetKeyRef, Variable: "fleetIdentity"}}
	}
	mark := FirewallMark(request.Identity.Context, request.Identity.Block, execution.ApplyOperation)
	services, digests, err := firewallContents(request, mark)
	if err != nil {
		return lifecycle.RunResult{}, request, nil, err
	}
	canonical, err := request.Canonical()
	if err != nil {
		return lifecycle.RunResult{}, request, nil, err
	}
	values["mark"], values["services"] = mark, services
	result, err := c.runner.Run(ctx, lifecycle.RunFor(execution, lifecycle.Invocation{
		Implementation: FirewallImplementation, Operation: operation, Variable: firewallVariable, Canonical: canonical, Placement: request.Placement,
		Materials: materials,
		Values:    values,
		Refusals:  firewallRefusals(execution.Context, request, removal), Deadline: 10 * time.Minute,
	}))
	return result, request, digests, err
}

func firewallContents(request FirewallRequest, mark string) (string, map[string]string, error) {
	type content struct {
		Addresses []string `json:"addresses,omitempty"`
		Content   string   `json:"content"`
		Name      string   `json:"name"`
		Networks  []string `json:"networks"`
	}
	contents := make([]content, 0, len(request.Services))
	digests := map[string]string{}
	for _, service := range request.Services {
		text := FirewallServiceContent(service, mark)
		sum := sha256.Sum256([]byte(text))
		digests[service.Name] = hex.EncodeToString(sum[:])
		contents = append(contents, content{Addresses: service.Addresses, Content: text, Name: service.Name, Networks: append([]string{}, service.Networks...)})
	}
	data, err := json.Marshal(contents)
	if err != nil {
		return "", nil, refusal("lifecycle.state", "the host firewall services cannot be encoded", "")
	}
	return string(data), digests, nil
}

func decodeFirewallEvidence(data []byte, digest string) (FirewallEvidence, error) {
	evidence, err := reconciliation.DecodeEvidence[FirewallEvidence](data, maxEvidenceBytes, "host firewall")
	if err != nil {
		return FirewallEvidence{}, err
	}
	if evidence.Request != digest || evidence.Version != FirewallEvidenceVersion || evidence.Retired {
		return FirewallEvidence{}, refusal("lifecycle.state", "host firewall evidence names another request or version", "")
	}
	return evidence, nil
}

func firewallEffect(evidence FirewallEvidence, request FirewallRequest, digests map[string]string, removal bool) reconciliation.EffectState {
	if !evidence.Ready && !removal || len(evidence.Services) != len(request.Services) {
		return reconciliation.EffectUnknown
	}
	if len(request.Services) == 0 {
		return reconciliation.EffectCompleted
	}
	complete, clean := true, true
	for index, service := range evidence.Services {
		if service.Name != request.Services[index].Name || !validMemberships(service.Memberships) {
			return reconciliation.EffectUnknown
		}
		switch service.State {
		case firewallOwned:
			if len(service.Digest) != sha256.Size*2 || strings.Trim(service.Digest, "0123456789abcdef") != "" {
				return reconciliation.EffectUnknown
			}
			clean = false
			complete = complete && service.Digest == digests[service.Name] && service.Covered && len(service.Memberships) != 0
		case firewallAbsent:
			if service.Digest != "" || len(service.Memberships) != 0 {
				return reconciliation.EffectUnknown
			}
			complete = false
		default:
			return reconciliation.EffectUnknown
		}
	}
	switch {
	case complete && removal:
		return reconciliation.EffectNoEffect
	case complete:
		return reconciliation.EffectCompleted
	case clean && removal:
		return reconciliation.EffectCompleted
	case clean:
		return reconciliation.EffectNoEffect
	}
	return reconciliation.EffectPartial
}

func validMemberships(memberships []FirewallMembership) bool {
	for index, membership := range memberships {
		if membership.Zone == "" || index > 0 && strings.Compare(memberships[index-1].Zone+"\x00"+memberships[index-1].Interface, membership.Zone+"\x00"+membership.Interface) >= 0 {
			return false
		}
	}
	return true
}

func firewallRefusals(contextName string, request FirewallRequest, removal bool) map[string]error {
	machine := "Machine/" + request.Identity.Object
	refused := func(message, remediation string) error {
		return machineRefusal(Request{Identity: Identity{Object: request.Identity.Object}})(message, remediation)
	}
	service := func(render func(service FirewallService) error) error {
		return updateRefusal{render: func(detail string) error {
			at := slices.IndexFunc(request.Services, func(service FirewallService) bool { return service.Name == detail })
			if at < 0 {
				return render(FirewallService{Name: "unknown", Networks: []string{"its required networks"}})
			}
			return render(request.Services[at])
		}}
	}
	verb := "apply"
	if removal {
		verb = "destroy"
	}
	again := "then run bootwright " + verb + " --context " + contextName + " again"
	return map[string]error{
		"host-firewall-foreign": service(func(s FirewallService) error {
			return refused(machine+" holds a firewalld service "+s.Name+" this context did not write, so its firewall was left unchanged",
				"remove /etc/firewalld/services/"+s.Name+".xml and its zone memberships from "+machine+" after confirming nothing else uses it, "+again)
		}),
		"host-firewall-no-interface": service(func(s FirewallService) error {
			return refused(machine+" has no interface whose network holds "+strings.Join(s.Networks, " or is ")+", which firewalld service "+s.Name+" admits traffic on",
				"give "+machine+" the declared listener address or required network in its network configuration, "+again)
		}),
		"host-firewall-inactive": refused("firewalld is not running on "+machine+", whose install profile enables it",
			"start firewalld on "+machine+" with systemctl enable --now firewalld, "+again),
		"host-firewall-failed": updateRefusal{render: func(detail string) error {
			cause := detailOr(detail, func(value string) bool {
				return slices.Contains([]string{"tooling", "reload", "zone", "write", "address", "runtime-drift", "enable", "ssh-admission"}, value)
			})
			return refused("firewalld refused a change on "+machine+": "+cause, "read the firewalld journal on "+machine+", correct it, "+again)
		}},
	}
}

func (FirewallCapability) PrecedesController(reconciliation.BlockDefinition) bool { return true }

// FirewallProviderRequirements names the providers which create a listener's
// addressed bridge. Daemon preparation intentionally has no such dependency.
func FirewallProviderRequirements(catalog api.Catalog, request FirewallRequest) []reconciliation.ObjectRef {
	var providers []string
	for _, service := range request.Services {
		for _, address := range service.Addresses {
			providers = append(providers, substrate.BridgeProviders(catalog, request.Identity.Object, address)...)
		}
	}
	slices.Sort(providers)
	var requires []reconciliation.ObjectRef
	for _, provider := range slices.Compact(providers) {
		requires = append(requires, reconciliation.ObjectRef{Kind: string(api.InfraProvider), Object: provider})
	}
	return requires
}
