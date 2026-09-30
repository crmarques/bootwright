package agentinstall

import (
	"context"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
)

// InstallCapability boots one cluster's nodes from the image its media block
// published and watches that cluster install. It owns what completion, replay
// and absence mean for the installation and nothing else: the image belongs to
// the media block, and each node's hardware to its own Machine block.
type InstallCapability struct {
	runner     Runner
	identities Identities
}

func NewInstall(runner Runner) InstallCapability { return InstallCapability{runner: runner} }

// WithIdentities returns a copy that reads each physical node's pin through
// identities, which composition binds to the pin's own owner.
func (c InstallCapability) WithIdentities(identities Identities) InstallCapability {
	c.identities = identities
	return c
}

const installVariablePrefix = "bootwright_cluster_install"

// Plan derives one block per selected cluster, waiting on that cluster's own
// media block. It reads no host, endpoint or Secret material.
func (c InstallCapability) Plan(ctx context.Context, input lifecycle.PlanInput) (lifecycle.CapabilityPlan, error) {
	if err := ctx.Err(); err != nil {
		return lifecycle.CapabilityPlan{}, err
	}
	if input.State == nil {
		return lifecycle.CapabilityPlan{}, refusal("lifecycle.state", "lifecycle planning requires compiled desired state", "")
	}
	_, requests, requirements, err := Requests(input.State.Effective(), input.Controller, input.Context.Name)
	if err != nil {
		return lifecycle.CapabilityPlan{}, err
	}
	plan := lifecycle.CapabilityPlan{Definitions: []reconciliation.BlockDefinition{}}
	digest := InstallContentDigest()
	for index, request := range requests {
		canonical, err := request.Canonical()
		if err != nil {
			return lifecycle.CapabilityPlan{}, err
		}
		definition := reconciliation.BlockDefinition{
			ID:          request.Identity.Block,
			Description: installDescription(input.Verb, request),
			Stage:       reconciliation.StageClusters,
			// The image this block boots from is its own capability's, so the
			// dependency is named rather than resolved from an API object:
			// both blocks realize the same cluster.
			Dependencies:   []string{MediaBlockID(request.Identity.Cluster)},
			Requires:       installRequires(requirements[index]),
			Impacts:        installImpacts(input.Verb, request),
			Groups:         installGroups(input.Verb, request),
			Kind:           Kind,
			Object:         request.Identity.Cluster,
			Implementation: InstallImplementation,
			ContentDigest:  digest,
			Request:        canonical,
		}
		// A node the substrate created holds nothing until this installs
		// something, and its disks are acknowledged when that substrate
		// destroys them. A physical node already holds whatever it holds, and
		// this installation is the moment that content is lost.
		if request.Physical() && input.Verb == reconciliation.Apply {
			definition.Consumes = []string{reconciliation.AuthorizationDataLoss}
		}
		plan.Definitions = append(plan.Definitions, definition)
		plan.Secrets = append(plan.Secrets, request.SecretReferences()...)
	}
	slices.Sort(plan.Secrets)
	plan.Secrets = slices.Compact(plan.Secrets)
	return plan, nil
}

// installRequires names the API objects this block waits for. Every node must
// be realized before it is booted, and the resolvers the nodes use must answer
// while they install.
func installRequires(needs Requirements) []reconciliation.ObjectRef {
	var references []reconciliation.ObjectRef
	for _, name := range needs.Machines {
		references = append(references, reconciliation.ObjectRef{Kind: "Machine", Object: name})
	}
	for _, name := range needs.DNSServers {
		references = append(references, reconciliation.ObjectRef{Kind: "DNSServer", Object: name})
	}
	for _, name := range needs.NTPServers {
		references = append(references, reconciliation.ObjectRef{Kind: "NTPServer", Object: name})
	}
	return references
}

func installDescription(verb reconciliation.Verb, request InstallRequest) string {
	if verb == reconciliation.Destroy {
		return "release the boot media of " + request.Identity.Cluster
	}
	return "install the cluster " + request.Identity.Cluster
}

func installImpacts(verb reconciliation.Verb, request InstallRequest) []string {
	var impacts []string
	for _, node := range request.Nodes {
		if verb == reconciliation.Destroy {
			impacts = append(impacts, "eject-media "+node.Machine)
			continue
		}
		impacts = append(impacts, "install-cluster "+node.Machine, "power-on "+node.Machine)
	}
	slices.Sort(impacts)
	return slices.Compact(impacts)
}

func installGroups(verb reconciliation.Verb, request InstallRequest) []reconciliation.Group {
	machines := request.Machines()
	steps := [][2]string{
		{"verify-resolution", "verify the controller resolves every name this cluster answers at"},
		{"boot-machines", "boot each node from the image its cluster published"},
		{"wait-bootstrap", "wait for the bootstrap control plane to hand over"},
		{"wait-install", "wait for the cluster to finish installing"},
		{"release-media", "eject the media and boot each node from its own disk"},
	}
	if verb == reconciliation.Destroy {
		steps = [][2]string{
			{"release-media", "eject the media every node still presents"},
			{"verify-absence", "verify no node presents this cluster's media"},
		}
	}
	out := make([]reconciliation.Group, 0, len(steps))
	for _, step := range steps {
		out = append(out, reconciliation.Group{ID: step[0], Description: step[1], Machines: machines})
	}
	return out
}

// Removal reads a frozen installation block as the removal of what it opened:
// the media each node still presents. The installed cluster leaves with those
// nodes' own disks, so removing this block consumes nothing.
func (c InstallCapability) Removal(ctx context.Context, block reconciliation.Block) (lifecycle.Removal, error) {
	request, err := DecodeInstallRequest(block.Request)
	if err != nil {
		return lifecycle.Removal{}, err
	}
	return lifecycle.Removal{
		Description: installDescription(reconciliation.Destroy, request),
		Impacts:     installImpacts(reconciliation.Destroy, request),
		Groups:      installGroups(reconciliation.Destroy, request),
	}, nil
}

func (c InstallCapability) Apply(ctx context.Context, execution lifecycle.Execution) (lifecycle.Result, error) {
	return c.mutate(ctx, execution, "apply")
}

func (c InstallCapability) Destroy(ctx context.Context, execution lifecycle.Execution) (lifecycle.Result, error) {
	return c.mutate(ctx, execution, "destroy")
}

// mutate offers the administrator access only from an apply whose evidence
// this run proved complete; a removal declares no output at all.
func (c InstallCapability) mutate(ctx context.Context, execution lifecycle.Execution, operation string) (lifecycle.Result, error) {
	unknown := lifecycle.Result{Outcome: reconciliation.OutcomeUnknown}
	request, err := c.prepare(ctx, execution, operation)
	if err != nil {
		return lifecycle.Result{Outcome: reconciliation.OutcomeFailed}, err
	}
	var outputs []lifecycle.OutputFile
	if operation == "apply" {
		outputs = kubeconfigOutputs()
	}
	result, err := c.runWithOutputs(ctx, execution, operation, request, outputs, "")
	if err != nil {
		lifecycle.ClearProduced(result.Produced)
		return lifecycle.Result{Outcome: lifecycle.AttemptOutcome(err)}, err
	}
	var outcome reconciliation.Outcome
	switch result.Outcome {
	case "changed":
		outcome = reconciliation.OutcomeChanged
	case "unchanged":
		outcome = reconciliation.OutcomeUnchanged
	default:
		lifecycle.ClearProduced(result.Produced)
		return unknown, refusal("lifecycle.state", "the installation adapter reported no usable outcome", "")
	}
	digest := execution.Block.RequestDigest
	if operation == "apply" {
		err = ValidateInstallPresence(result.Evidence, request, digest)
	} else {
		err = ValidateInstallAbsence(result.Evidence, digest)
	}
	if err != nil {
		lifecycle.ClearProduced(result.Produced)
		return unknown, err
	}
	return lifecycle.Result{Outcome: outcome, Evidence: result.Evidence, Produced: offered(result.Produced, operation == "apply")}, nil
}

func kubeconfigOutputs() []lifecycle.OutputFile {
	return []lifecycle.OutputFile{{Name: KubeconfigOutput, Variable: KubeconfigOutput}}
}

// offered keeps the administrator access when the run proved completion and
// clears everything it discards.
func offered(produced []lifecycle.Produced, proved bool) []lifecycle.Produced {
	var kept []lifecycle.Produced
	for _, output := range produced {
		if proved && output.Name == KubeconfigOutput && len(kept) == 0 {
			kept = append(kept, output)
			continue
		}
		output.Material.Clear()
	}
	return kept
}

// Observe is read-only. The cluster this operation installed, answering at the
// declared release with every declared node and no media left inserted, is
// positive completion; nothing running and nothing answering is positive no
// effect; that same cluster answering with the completion not yet true is a
// positive partial realization the next attempt converges.
func (c InstallCapability) Observe(ctx context.Context, execution lifecycle.Execution) (lifecycle.Observation, error) {
	return c.observe(ctx, execution, "", kubeconfigOutputs(), func(evidence []byte, request InstallRequest, digest string) reconciliation.EffectState {
		switch {
		case ValidateInstallPresence(evidence, request, digest) == nil:
			return reconciliation.EffectCompleted
		case ValidateInstallNoEffect(evidence, digest) == nil:
			return reconciliation.EffectNoEffect
		case ValidateInstallPartial(evidence, digest) == nil:
			return reconciliation.EffectPartial
		}
		return reconciliation.EffectUnknown
	})
}

// observesRemoval scopes an observation to what a removal takes back: the
// adapter reads the media each node's controller presents, and not the
// cluster, which a removal leaves running.
const observesRemoval = "removal"

// ObserveRemoval reads, for what a removal proves, an observation of the
// media each node presents. The removal ejects whatever each node presents,
// and a completed installation already presents none, so no node presenting
// media is its completion; one of this cluster's own nodes presenting any
// image, this cluster's or another, is a positive partial removal the next
// attempt converges; and a node that cannot be read fails the observation,
// which stays unknown.
func (c InstallCapability) ObserveRemoval(ctx context.Context, execution lifecycle.Execution) (lifecycle.Observation, error) {
	return c.observe(ctx, execution, observesRemoval, nil, func(evidence []byte, request InstallRequest, digest string) reconciliation.EffectState {
		switch {
		case ValidateInstallReleased(evidence, digest) == nil:
			return reconciliation.EffectCompleted
		case ValidateInstallReleasePartial(evidence, request, digest) == nil:
			return reconciliation.EffectPartial
		}
		return reconciliation.EffectUnknown
	})
}

// observe runs the read-only observation operation, scoped by observes, and
// reads its evidence for the verb the block was frozen for. Only the apply's
// observation declares the administrator access, and offers it only when it
// reads the installation complete.
func (c InstallCapability) observe(ctx context.Context, execution lifecycle.Execution, observes string, outputs []lifecycle.OutputFile, read func([]byte, InstallRequest, string) reconciliation.EffectState) (lifecycle.Observation, error) {
	unknown := lifecycle.Observation{Effect: reconciliation.EffectUnknown}
	request, err := c.prepare(ctx, execution, "observe")
	if err != nil {
		return unknown, err
	}
	result, err := c.runWithOutputs(ctx, execution, "observe", request, outputs, observes)
	if err != nil {
		lifecycle.ClearProduced(result.Produced)
		recordObservationFailure(ctx, execution, err)
		return unknown, nil
	}
	effect := read(result.Evidence, request, execution.Block.RequestDigest)
	produced := offered(result.Produced, len(outputs) != 0 && effect == reconciliation.EffectCompleted)
	return lifecycle.Observation{Effect: effect, Evidence: result.Evidence, Produced: produced}, nil
}

// Quiescent is derived rather than probed. This block owns the media each node
// presents and the controller-side state of the installation, which a running
// cluster no longer reads; a node still running is probed by its own Machine
// block in the same removal.
func (InstallCapability) Quiescent(context.Context, lifecycle.Probe) (lifecycle.Quiescence, error) {
	return lifecycle.Quiescence{State: lifecycle.Quiescent, Reason: "its nodes are probed in this removal"}, nil
}

// Unsupported names every selected cluster this capability cannot install, so
// the operation refuses before registration.
func (InstallCapability) Unsupported(state *compilation.State) []string {
	if state == nil {
		return nil
	}
	return Unsupported(state.Effective())
}

func (c InstallCapability) prepare(ctx context.Context, execution lifecycle.Execution, operation string) (InstallRequest, error) {
	if err := ctx.Err(); err != nil {
		return InstallRequest{}, err
	}
	if c.runner == nil {
		return InstallRequest{}, refusal("lifecycle.state", "the installation adapter is not configured", "")
	}
	request, err := DecodeInstallRequest(execution.Block.Request)
	if err != nil {
		return InstallRequest{}, err
	}
	if operation == "apply" {
		if err := refusedContinuation(request); err != nil {
			return InstallRequest{}, err
		}
	}
	return request, nil
}

// refusedContinuation says why this executable does not apply a request an
// earlier executable froze, or nothing when it would plan that request itself.
// Planning refuses a physical node before registration, but an operation
// registered before that refusal still carries one, and an apply boots exactly
// the nodes that were frozen. A destroy and an observation are never refused:
// the one ejects media and the other reads.
func refusedContinuation(request InstallRequest) error {
	for _, node := range request.Nodes {
		if node.Physical {
			return refusal("lifecycle.state", "this operation froze Machine/"+node.Machine+" as a physical node of ContainerCluster/"+
				request.Identity.Cluster+", which this executable refuses",
				"run bootwright destroy to end this operation, then plan it again under this executable")
		}
	}
	return nil
}

func (c InstallCapability) run(ctx context.Context, execution lifecycle.Execution, operation string, request InstallRequest) (lifecycle.RunResult, error) {
	return c.runWithOutputs(ctx, execution, operation, request, nil, "")
}

func (c InstallCapability) runWithOutputs(ctx context.Context, execution lifecycle.Execution, operation string, request InstallRequest, outputs []lifecycle.OutputFile, observes string) (lifecycle.RunResult, error) {
	canonical, err := request.Canonical()
	if err != nil {
		return lifecycle.RunResult{}, err
	}
	values := map[string]string{}
	if observes != "" {
		values["observes"] = observes
	}
	for executable, tool := range map[string]Tool{
		installerTool: request.Tool,
		"oc":          {Compatibility: request.Release.Distribution, Kind: clientTool, Version: request.Release.Version},
	} {
		path, err := ToolPath(ctx, execution, tool, executable)
		if err != nil {
			return lifecycle.RunResult{}, err
		}
		values[strings.ReplaceAll(executable, "-", "")] = path
	}
	if operation == "apply" {
		pins, err := c.pinValues(execution, request)
		if err != nil {
			return lifecycle.RunResult{}, err
		}
		maps.Copy(values, pins)
	}
	// The attempt adds the placement's identity and host key, so only the
	// nodes' own credentials are listed here.
	var materials []lifecycle.MaterialFile
	for index, node := range request.Nodes {
		materials = append(materials,
			lifecycle.MaterialFile{
				Name: "bmc-user-" + node.Machine, Part: secrets.UsernamePart,
				Secret: node.Controller.CredentialsRef, Variable: "controllerUser" + nodeVariable(index),
			},
			lifecycle.MaterialFile{
				Name: "bmc-password-" + node.Machine, Part: secrets.PasswordPart,
				Secret: node.Controller.CredentialsRef, Variable: "controllerPassword" + nodeVariable(index),
			})
	}
	// The deadline follows the budgets and nodes this request froze, not this
	// build's.
	return c.runner.Run(ctx, lifecycle.RunFor(execution, lifecycle.Invocation{
		Implementation: InstallImplementation, Operation: operation, Variable: installVariablePrefix,
		Canonical: canonical, Placement: request.Placement, Materials: materials, Values: values,
		Outputs: outputs, Deadline: request.Deadline(),
	}))
}

// pinValues carries the identity each physical node's own Machine block proved
// earlier in this operation, under that node's position, so its pre-boot proof
// refuses a machine that answers as another system. A node its substrate
// created, and a Machine with no pin, carry nothing.
func (c InstallCapability) pinValues(execution lifecycle.Execution, request InstallRequest) (map[string]string, error) {
	values := map[string]string{}
	for index, node := range request.Nodes {
		if !node.Physical {
			continue
		}
		if c.identities == nil {
			return nil, refusal("lifecycle.state", "the reader of Machine/"+node.Machine+"'s pinned identity is not configured", "")
		}
		pin, pinned, err := c.identities.PinnedIdentity(node.Machine, execution.Proved)
		if err != nil {
			return nil, err
		}
		if pinned {
			maps.Copy(values, pin.PinValues(nodeVariable(index)))
		}
	}
	return values, nil
}

// nodeVariable names one node's material by its position in the frozen node
// order, so the adapter reads the credential of the node it is booting without
// a name the request would have to make safe for a variable.
func nodeVariable(index int) string { return "Node" + strconv.Itoa(index) }
