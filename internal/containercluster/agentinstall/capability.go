package agentinstall

import (
	"context"
	"slices"
	"strconv"
	"strings"

	controllerscope "github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
)

// MediaCapability builds and publishes the image one cluster's nodes boot. It
// owns what completion, replay and absence mean for that image and nothing
// else: the cluster it serves belongs to the installation block.
type MediaCapability struct{ runner Runner }

func NewMedia(runner Runner) MediaCapability { return MediaCapability{runner: runner} }

const mediaVariablePrefix = "bootwright_cluster_media"

// Plan derives one block per selected cluster. It reads no host, endpoint or
// Secret material and performs no effect.
func (c MediaCapability) Plan(ctx context.Context, input lifecycle.PlanInput) (lifecycle.CapabilityPlan, error) {
	if err := ctx.Err(); err != nil {
		return lifecycle.CapabilityPlan{}, err
	}
	if input.State == nil {
		return lifecycle.CapabilityPlan{}, refusal("lifecycle.state", "lifecycle planning requires compiled desired state", "")
	}
	requests, _, requirements, err := Requests(input.State.Effective(), input.Controller, input.Context.Name)
	if err != nil {
		return lifecycle.CapabilityPlan{}, err
	}
	plan := lifecycle.CapabilityPlan{Definitions: []reconciliation.BlockDefinition{}}
	digest := MediaContentDigest()
	for index, request := range requests {
		canonical, err := request.Canonical()
		if err != nil {
			return lifecycle.CapabilityPlan{}, err
		}
		plan.Definitions = append(plan.Definitions, reconciliation.BlockDefinition{
			ID:             request.Identity.Block,
			Description:    mediaDescription(input.Verb, request),
			Stage:          reconciliation.StageClusters,
			Requires:       mediaRequires(requirements[index]),
			Impacts:        mediaImpacts(input.Verb, request),
			Groups:         mediaGroups(input.Verb, request),
			Kind:           Kind,
			Object:         request.Identity.Cluster,
			Implementation: MediaImplementation,
			ContentDigest:  digest,
			Request:        canonical,
		})
		plan.Secrets = append(plan.Secrets, request.SecretReferences()...)
		plan.Reservations = append(plan.Reservations, prerequisites.HostReservation{
			Context: input.Context.Name, Kind: "cluster-media",
			Service: request.Identity.Cluster, Keys: request.ReservationKeys(),
		})
	}
	slices.Sort(plan.Secrets)
	plan.Secrets = slices.Compact(plan.Secrets)
	return plan, nil
}

// mediaRequires names the API objects this block waits for, never their block
// identities. The image is published into a managed artifact server, so that
// server answers before anything is written beneath its root.
func mediaRequires(needs Requirements) []reconciliation.ObjectRef {
	references := make([]reconciliation.ObjectRef, 0, len(needs.ArtifactServers))
	for _, name := range needs.ArtifactServers {
		references = append(references, reconciliation.ObjectRef{Kind: "ArtifactServer", Object: name})
	}
	return references
}

func mediaDescription(verb reconciliation.Verb, request MediaRequest) string {
	if verb == reconciliation.Destroy {
		return "remove the boot media of " + request.Identity.Cluster
	}
	return "build the boot media of " + request.Identity.Cluster
}

func mediaImpacts(verb reconciliation.Verb, request MediaRequest) []string {
	published, work := "publish-content ", "create-path "
	if verb == reconciliation.Destroy {
		published, work = "remove-content ", "remove-path "
	}
	impacts := []string{published + request.Image.Path, work + request.WorkRoot}
	slices.Sort(impacts)
	return slices.Compact(impacts)
}

func mediaGroups(verb reconciliation.Verb, request MediaRequest) []reconciliation.Group {
	machines := []string{request.Placement.Machine}
	steps := [][2]string{
		{"build-image", "build the image this cluster's nodes boot"},
		{"publish-image", "publish it where only those nodes can fetch it"},
	}
	if verb == reconciliation.Destroy {
		steps = [][2]string{
			{"remove-published", "remove the published image and the installer's work area"},
			{"verify-absence", "verify every published file is gone"},
		}
	}
	out := make([]reconciliation.Group, 0, len(steps))
	for _, step := range steps {
		out = append(out, reconciliation.Group{ID: step[0], Description: step[1], Machines: machines})
	}
	return out
}

// Removal reads a frozen media block as the removal of what it published. The
// cluster that booted from the image keeps running, so removing this block
// consumes nothing.
func (c MediaCapability) Removal(ctx context.Context, block reconciliation.Block) (lifecycle.Removal, error) {
	request, err := DecodeMediaRequest(block.Request)
	if err != nil {
		return lifecycle.Removal{}, err
	}
	return lifecycle.Removal{
		Description: mediaDescription(reconciliation.Destroy, request),
		Impacts:     mediaImpacts(reconciliation.Destroy, request),
		Groups:      mediaGroups(reconciliation.Destroy, request),
	}, nil
}

func (c MediaCapability) Apply(ctx context.Context, execution lifecycle.Execution) (lifecycle.Result, error) {
	return c.mutate(ctx, execution, "apply")
}

func (c MediaCapability) Destroy(ctx context.Context, execution lifecycle.Execution) (lifecycle.Result, error) {
	return c.mutate(ctx, execution, "destroy")
}

func (c MediaCapability) mutate(ctx context.Context, execution lifecycle.Execution, operation string) (lifecycle.Result, error) {
	unknown := lifecycle.Result{Outcome: reconciliation.OutcomeUnknown}
	request, err := c.prepare(ctx, execution)
	if err != nil {
		return lifecycle.Result{Outcome: reconciliation.OutcomeFailed}, err
	}
	result, err := c.run(ctx, execution, operation, request)
	if err != nil {
		return lifecycle.Result{Outcome: lifecycle.AttemptOutcome(err)}, err
	}
	var outcome reconciliation.Outcome
	switch result.Outcome {
	case "changed":
		outcome = reconciliation.OutcomeChanged
	case "unchanged":
		outcome = reconciliation.OutcomeUnchanged
	default:
		return unknown, refusal("lifecycle.state", "the boot-media adapter reported no usable outcome", "")
	}
	digest := execution.Block.RequestDigest
	if operation == "apply" {
		err = ValidateMediaPresence(result.Evidence, request, digest)
	} else {
		err = ValidateMediaAbsence(result.Evidence, digest)
	}
	if err != nil {
		return unknown, err
	}
	return lifecycle.Result{Outcome: outcome, Evidence: result.Evidence}, nil
}

// Observe is read-only. A published image built from this exact request by the
// declared release's installer is positive completion; nothing published at all
// is positive no effect; anything of this block's own left behind is a positive
// partial realization the next attempt converges by building again.
func (c MediaCapability) Observe(ctx context.Context, execution lifecycle.Execution) (lifecycle.Observation, error) {
	return c.observe(ctx, execution, reconciliation.Apply)
}

// ObserveRemoval reads the same observation for what a removal proves. The
// observation never reports the absence form, so neither the image nor the
// work area present is the removal's completion; the image this request
// describes is positive no effect; and anything of this block's own left is a
// positive partial removal the next attempt converges.
func (c MediaCapability) ObserveRemoval(ctx context.Context, execution lifecycle.Execution) (lifecycle.Observation, error) {
	return c.observe(ctx, execution, reconciliation.Destroy)
}

// mediaEffect is the one reading of a boot-media observation for the verb the
// block was frozen for: an apply's checks are its presence, no effect and
// partial forms, a removal's its no effect, presence and partial forms, each in
// that order. When none accepts the evidence the effect is unknown, and the
// refusal that decided it comes back with it: the presence check's for an
// apply, the partial check's for a removal.
func mediaEffect(verb reconciliation.Verb, evidence []byte, request MediaRequest, digest string) (reconciliation.EffectState, error) {
	if verb == reconciliation.Destroy {
		switch {
		case ValidateMediaNoEffect(evidence, digest) == nil:
			return reconciliation.EffectCompleted, nil
		case ValidateMediaPresence(evidence, request, digest) == nil:
			return reconciliation.EffectNoEffect, nil
		}
		if err := ValidateMediaPartial(evidence, digest); err != nil {
			return reconciliation.EffectUnknown, err
		}
		return reconciliation.EffectPartial, nil
	}
	presence := ValidateMediaPresence(evidence, request, digest)
	switch {
	case presence == nil:
		return reconciliation.EffectCompleted, nil
	case ValidateMediaNoEffect(evidence, digest) == nil:
		return reconciliation.EffectNoEffect, nil
	case ValidateMediaPartial(evidence, digest) == nil:
		return reconciliation.EffectPartial, nil
	}
	return reconciliation.EffectUnknown, presence
}

// observe runs the one read-only observation both resolutions share and reads
// its evidence for the verb the block was frozen for.
func (c MediaCapability) observe(ctx context.Context, execution lifecycle.Execution, verb reconciliation.Verb) (lifecycle.Observation, error) {
	unknown := lifecycle.Observation{Effect: reconciliation.EffectUnknown}
	request, err := c.prepare(ctx, execution)
	if err != nil {
		return unknown, err
	}
	result, err := c.run(ctx, execution, "observe", request)
	if err != nil {
		return unknown, err
	}
	effect, _ := mediaEffect(verb, result.Evidence, request, execution.Block.RequestDigest)
	return lifecycle.Observation{Effect: effect, Evidence: result.Evidence}, nil
}

// Keeps names the custody entry of the administrator kubeconfig the cluster's
// installation keeps in the work area this block's removal deletes: the one a
// completed installation's capture fills.
func (MediaCapability) Keeps(block reconciliation.Block) (lifecycle.KeptEntry, bool, error) {
	request, err := DecodeMediaRequest(block.Request)
	if err != nil {
		return lifecycle.KeptEntry{}, false, err
	}
	return lifecycle.KeptEntry{Block: InstallBlockID(request.Identity.Cluster), Name: KubeconfigOutput}, true, nil
}

// Keep runs the read-only observation with the administrator access declared
// as its one output, so the adapter hands over the kubeconfig the
// installation kept in the work area, whole and within its bound, whatever
// the installation proved. A work area that holds none hands over nothing.
func (c MediaCapability) Keep(ctx context.Context, execution lifecycle.Execution) ([]lifecycle.Produced, error) {
	request, err := c.prepare(ctx, execution)
	if err != nil {
		return nil, err
	}
	result, err := c.runWithOutputs(ctx, execution, "observe", request, kubeconfigOutputs())
	if err != nil {
		lifecycle.ClearProduced(result.Produced)
		return nil, err
	}
	return result.Produced, nil
}

// Quiescent is derived rather than probed. This block owns a published image
// and the installer's own work area, which an installed cluster no longer
// reads; a node still booting from it is one whose own block is probed in the
// same removal.
func (MediaCapability) Quiescent(context.Context, lifecycle.Probe) (lifecycle.Quiescence, error) {
	return lifecycle.Quiescence{State: lifecycle.Quiescent, Reason: "its nodes are probed in this removal"}, nil
}

// Unsupported refuses every selected cluster this capability cannot install,
// with its reason and remedy, so the operation refuses before registration.
func (MediaCapability) Unsupported(state *compilation.State) []lifecycle.Refusal {
	if state == nil {
		return nil
	}
	return Refusals(state.Effective())
}

func (c MediaCapability) prepare(ctx context.Context, execution lifecycle.Execution) (MediaRequest, error) {
	if err := ctx.Err(); err != nil {
		return MediaRequest{}, err
	}
	if c.runner == nil {
		return MediaRequest{}, refusal("lifecycle.state", "the boot-media adapter is not configured", "")
	}
	return DecodeMediaRequest(execution.Block.Request)
}

func (c MediaCapability) run(ctx context.Context, execution lifecycle.Execution, operation string, request MediaRequest) (lifecycle.RunResult, error) {
	return c.runWithOutputs(ctx, execution, operation, request, nil)
}

func (c MediaCapability) runWithOutputs(ctx context.Context, execution lifecycle.Execution, operation string, request MediaRequest, outputs []lifecycle.OutputFile) (lifecycle.RunResult, error) {
	canonical, err := request.Canonical()
	if err != nil {
		return lifecycle.RunResult{}, err
	}
	values := map[string]string{}
	var materials []lifecycle.MaterialFile
	if operation == "apply" {
		installer, err := ToolPath(ctx, execution, request.Tool, installerTool)
		if err != nil {
			return lifecycle.RunResult{}, err
		}
		key, err := publicHalf(execution, request.SSHKeyRef)
		if err != nil {
			return lifecycle.RunResult{}, err
		}
		values["installer"], values["sshKey"] = installer, key
		// The fetch that proves the publication verifies the listener against
		// the certificate its Secret binds to this operation, never against
		// the copy the server installed, which proves only itself.
		materials = append(materials, lifecycle.MaterialFile{
			Name: "pull-secret", Part: secrets.ValuePart, Secret: request.PullSecretRef, Variable: "pullSecret",
		}, lifecycle.MaterialFile{
			Name: "artifact-ca", Part: secrets.CertificatePart, Secret: request.TLSCertificateRef, Variable: "artifactCertificate",
		})
		for index, reference := range request.TrustBundleRefs {
			materials = append(materials, lifecycle.MaterialFile{
				Name:     "trust-" + strconv.Itoa(index),
				Part:     secrets.CertificatePart,
				Secret:   reference,
				Variable: "trustBundle" + strconv.Itoa(index),
			})
		}
	}
	// The deadline follows the budgets this request froze, not this build's.
	return c.runner.Run(ctx, lifecycle.RunFor(execution, lifecycle.Invocation{
		Implementation: MediaImplementation, Operation: operation, Variable: mediaVariablePrefix,
		Canonical: canonical, Placement: request.Placement, Materials: materials, Values: values,
		Outputs: outputs, Deadline: request.Deadline(),
	}))
}

// ToolPath is the exact executable the controller stage published for one
// declared release. A search path is never authority: an installer is the
// release pin, so the one this block runs is the one this context's own graph
// selected, and the client that reads the installed cluster back comes from
// the same closure.
func ToolPath(ctx context.Context, execution lifecycle.Execution, tool Tool, executable string) (string, error) {
	if execution.LocateTool == nil {
		return "", refusal("controller.state", "the retained controller areas are unavailable",
			"run bootwright setup, then apply --stage controller")
	}
	return execution.LocateTool(ctx, controllerscope.InstalledTool{
		Kind: tool.Kind, Compatibility: tool.Compatibility, Version: tool.Version, Executable: executable,
	})
}

// publicHalf reads the public half of one bound key pair. A private half never
// leaves its binding as a value.
func publicHalf(execution lifecycle.Execution, reference string) (string, error) {
	material, ok := execution.Material[reference]
	if !ok {
		return "", refusal("secret.store", "the bound cluster administration key is not available to this attempt",
			"repeat the operation so its Secret bindings are reopened")
	}
	value, ok := material.Part(secrets.PublicKeyPart)
	if !ok || len(value) == 0 {
		return "", refusal("secret.part", "the bound cluster administration key carries no public half",
			"repeat the operation so its Secret bindings are reopened")
	}
	return strings.TrimRight(string(value), "\n"), nil
}
