package agentinstall

import (
	"context"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/crmarques/bootwright/internal/controller/evidence"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/infrastructureservices"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/substrate"
)

// MediaCapability builds and publishes the image one cluster's nodes boot. It
// owns what completion, replay and absence mean for that image and nothing
// else: the cluster it serves belongs to the installation block.
type MediaCapability struct {
	runner        Runner
	authorizedKey AuthorizedKey
	identities    Identities
}

func NewMedia(runner Runner, authorizedKey AuthorizedKey) MediaCapability {
	return MediaCapability{runner: runner, authorizedKey: authorizedKey}
}

// WithIdentities supplies the durable Machine proof used by logical-volume roots.
func (c MediaCapability) WithIdentities(identities Identities) MediaCapability {
	c.identities = identities
	return c
}

const mediaVariablePrefix = "bootwright_cluster_media"

type derivedRequests struct {
	media        []MediaRequest
	installs     []InstallRequest
	requirements []Requirements
}

func plannedRequests(input lifecycle.PlanInput) ([]MediaRequest, []InstallRequest, []Requirements, error) {
	derived, err := lifecycle.Derived(input, "containercluster/agentinstall.requests", func() (derivedRequests, error) {
		media, installs, requirements, err := Requests(input.State.Effective(), input.Controller, input.Context.Name)
		return derivedRequests{media: media, installs: installs, requirements: requirements}, err
	})
	if err != nil {
		return nil, nil, nil, err
	}
	return slices.Clone(derived.media), slices.Clone(derived.installs), slices.Clone(derived.requirements), nil
}

// Plan derives one block per selected cluster. It reads no host, endpoint or
// Secret material and performs no effect.
func (c MediaCapability) Plan(ctx context.Context, input lifecycle.PlanInput) (lifecycle.CapabilityPlan, error) {
	if err := ctx.Err(); err != nil {
		return lifecycle.CapabilityPlan{}, err
	}
	if input.State == nil {
		return lifecycle.CapabilityPlan{}, openshiftPlanStateMissing.Fail("lifecycle planning requires compiled desired state", "")
	}
	requests, _, requirements, err := plannedRequests(input)
	if err != nil {
		return lifecycle.CapabilityPlan{}, err
	}
	plan := lifecycle.CapabilityPlan{Definitions: []reconciliation.BlockDefinition{}}
	for index, request := range requests {
		definition, err := mediaDefinition(request, requirements[index], input.Verb)
		if err != nil {
			return lifecycle.CapabilityPlan{}, err
		}
		plan.Definitions = append(plan.Definitions, definition)
		plan.Secrets = append(plan.Secrets, request.SecretReferences()...)
		plan.Reservations = append(plan.Reservations, infrastructureservices.HostReservation{
			Context: input.Context.Name, Kind: infrastructureservices.ClusterMediaReservation,
			Service: request.Identity.Cluster, ObjectKind: Kind, Keys: request.ReservationKeys(),
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
	for _, name := range needs.VolumeMachines {
		references = append(references, reconciliation.ObjectRef{Kind: "Machine", Object: name, Family: substrate.MachineFamily})
	}
	for _, name := range needs.Proxies {
		references = append(references, reconciliation.ObjectRef{Kind: "Proxy", Object: name})
	}
	return append(references, releaseRequires(needs)...)
}

func releaseRequires(needs Requirements) []reconciliation.ObjectRef {
	if needs.MirroredRelease == "" {
		return nil
	}
	return []reconciliation.ObjectRef{{Kind: Kind, Object: needs.MirroredRelease, Family: infrastructureservices.MirrorFillFamily}}
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
	impacts := []string{work + request.WorkRoot}
	if request.DeliveryKind != "push" {
		impacts = append(impacts, published+request.Image.Path)
	}
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
	if request.DeliveryKind == "push" {
		if verb == reconciliation.Destroy {
			steps[0][1], steps[1][1] = "remove the built image and installer work area", "verify the work area is gone"
		} else {
			steps = steps[:1]
		}
	}
	return lifecycle.StepGroups(steps, machines)
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

// KeptHostsRefusal admits the media's removal while its nodes stay, since it
// touches only what the controller published; its installation's answer decides.
func (MediaCapability) KeptHostsRefusal(reconciliation.Block, lifecycle.RemovalRecords) string {
	return ""
}

func (c MediaCapability) KeptHostsRemoval(ctx context.Context, block reconciliation.Block) (lifecycle.Removal, error) {
	return c.Removal(ctx, block)
}

func (c MediaCapability) Apply(ctx context.Context, execution lifecycle.Execution) (lifecycle.Result, error) {
	return lifecycle.MutateBlock(ctx, c.mutation("apply", mediaProof), execution, "apply")
}

func (c MediaCapability) Destroy(ctx context.Context, execution lifecycle.Execution) (lifecycle.Result, error) {
	return lifecycle.MutateBlock(ctx, c.mutation("destroy", mediaProof), execution, "destroy")
}

func (c MediaCapability) mutation(operation string, reading func(MediaRequest, string) lifecycle.EffectReading) lifecycle.Mutation[MediaRequest] {
	return lifecycle.Mutation[MediaRequest]{
		Prepare: c.prepare, Reading: reading,
		Run: func(ctx context.Context, execution lifecycle.Execution, _ string, request MediaRequest) (lifecycle.RunResult, error) {
			return c.run(ctx, execution, operation, request)
		},
		Unusable: openshiftOutcomeUnusable.Fail("the boot-media adapter reported no usable outcome", ""),
	}
}

// Observe is read-only. A published image built from this exact request by the
// declared release's installer is positive completion; nothing published at all
// is positive no effect; anything of this block's own left behind is a positive
// partial realization the next attempt converges by building again.
func (c MediaCapability) Observe(ctx context.Context, execution lifecycle.Execution) (lifecycle.Observation, error) {
	return lifecycle.ObserveBlock(ctx, c.mutation("observe", mediaReading), execution, reconciliation.Apply)
}

// ObserveRemoval reads the same observation for what a removal proves. The
// observation never reports the absence form, so neither the image nor the
// work area present is the removal's completion; the image this request
// describes is positive no effect; and anything of this block's own left is a
// positive partial removal the next attempt converges.
func (c MediaCapability) ObserveRemoval(ctx context.Context, execution lifecycle.Execution) (lifecycle.Observation, error) {
	return lifecycle.ObserveBlock(ctx, c.mutation("observe", mediaReading), execution, reconciliation.Destroy)
}

// mediaProof is what a mutation's evidence must prove: the published image for
// an apply, and the published image and work area gone for a destroy.
func mediaProof(request MediaRequest, digest string) lifecycle.EffectReading {
	return lifecycle.EffectReading{
		Presence: func(evidence []byte) error { return ValidateMediaPresence(evidence, request, digest) },
		Absence:  func(evidence []byte) error { return ValidateMediaAbsence(evidence, digest) },
	}
}

// mediaReading reads a boot-media observation for the verb the block was
// frozen for. Its no effect form is a removal's completion, and its presence
// form a removal's no effect.
func mediaReading(request MediaRequest, digest string) lifecycle.EffectReading {
	return lifecycle.EffectReading{
		Presence:   func(evidence []byte) error { return ValidateMediaPresence(evidence, request, digest) },
		Absence:    func(evidence []byte) error { return ValidateMediaNoEffect(evidence, digest) },
		Partial:    func(evidence []byte) error { return ValidateMediaPartial(evidence, digest) },
		Unremoved:  func(evidence []byte) error { return ValidateMediaPresence(evidence, request, digest) },
		Unfinished: func(evidence []byte) error { return ValidateMediaPartial(evidence, digest) },
	}
}

func mediaEffect(verb reconciliation.Verb, evidence []byte, request MediaRequest, digest string) (reconciliation.EffectState, error) {
	return lifecycle.ReadEffect(mediaReading(request, digest), verb, evidence)
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
	result, err := c.runWithOutputs(ctx, execution, "observe", request, []lifecycle.OutputFile{{Name: KubeconfigOutput, Variable: KubeconfigOutput}})
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
		return MediaRequest{}, openshiftAdapterUnconfigured.Fail("the boot-media adapter is not configured", "")
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
	values := map[string]string{"incarnation": execution.Incarnation}
	if operation == "apply" {
		resolved, err := c.volumeValues(execution, request)
		if err != nil {
			return lifecycle.RunResult{}, err
		}
		maps.Copy(values, resolved)
	}
	var materials []lifecycle.MaterialFile
	if operation == "apply" {
		installer, err := ToolPath(ctx, execution, request.Tool, request.Tool.Kind)
		if err != nil {
			return lifecycle.RunResult{}, err
		}
		client, err := ToolPath(ctx, execution, Tool{Compatibility: request.Release.Distribution, Kind: clientTool, Version: request.Release.Version}, "oc")
		if err != nil {
			return lifecycle.RunResult{}, err
		}
		key, err := c.publicHalf(execution, request.SSHKeyRef)
		if err != nil {
			return lifecycle.RunResult{}, err
		}
		if request.InstallConfig["fips"] == true && strings.HasPrefix(strings.TrimSpace(key), "ssh-ed25519 ") {
			return lifecycle.RunResult{}, refusal("controller.unsupported", "FIPS installation refuses the ssh-ed25519 key bound to Secret/"+request.SSHKeyRef, "bind an RSA or ECDSA administration key and repeat the apply")
		}
		values["installer"], values["oc"], values["sshKey"] = installer, client, key
		// The fetch that proves the publication verifies the listener against
		// the certificate its Secret binds to this operation, never against
		// the copy the server installed, which proves only itself.
		materials = append(materials, lifecycle.MaterialFile{
			Name: "pull-secret", Part: secrets.ValuePart, Secret: request.PullSecretRef, Variable: "pullSecret",
		})
		if request.DeliveryKind != "push" {
			materials = append(materials, lifecycle.MaterialFile{
				Name: "artifact-ca", Part: secrets.CertificatePart, Secret: request.TLSCertificateRef, Variable: "artifactCertificate",
			})
		}

		if proxy := request.InstallProxy; proxy != nil && proxy.CredentialsRef != "" {
			materials = append(materials,
				lifecycle.MaterialFile{Name: "proxy-user", Part: secrets.UsernamePart, Secret: proxy.CredentialsRef, Variable: "proxyUsername"},
				lifecycle.MaterialFile{Name: "proxy-password", Part: secrets.PasswordPart, Secret: proxy.CredentialsRef, Variable: "proxyPassword"})
		}
		if mirror := request.RegistryMirror; mirror != nil && mirror.CredentialsRef != "" {
			if mirror.CredentialsType == "usernamePassword" {
				materials = append(materials,
					lifecycle.MaterialFile{Name: "mirror-username", Part: secrets.UsernamePart, Secret: mirror.CredentialsRef, Variable: "mirrorUsername"},
					lifecycle.MaterialFile{Name: "mirror-password", Part: secrets.PasswordPart, Secret: mirror.CredentialsRef, Variable: "mirrorPassword"})
			} else {
				materials = append(materials, lifecycle.MaterialFile{Name: "mirror-auth", Part: secrets.ValuePart, Secret: mirror.CredentialsRef, Variable: "mirrorAuth"})
			}
		}
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
		Outputs: outputs, GroupLog: installerLogGroupNote(operation, MediaImplementation, request.WorkRoot), Deadline: request.Deadline(),
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
			"run bootwright install, then bootwright apply --context "+execution.Context+" --stage controller")
	}
	return execution.LocateTool(ctx, evidence.InstalledTool{
		Kind: tool.Kind, Compatibility: tool.Compatibility, Version: tool.Version, Executable: executable,
	})
}

// publicHalf reads the public key from the bound key pair or opaque value.
// A private half never leaves its binding as a value.
func (c MediaCapability) publicHalf(execution lifecycle.Execution, reference string) (string, error) {
	material, ok := execution.Material[reference]
	if !ok {
		return "", refusal("secret.store", "the bound cluster administration key is not available to this attempt",
			lifecycle.ReopenBindings(execution.Context, execution.Continuation))
	}
	if c.authorizedKey == nil {
		return "", openshiftAdapterUnconfigured.Fail("the public-key reader is not configured", "")
	}
	return c.authorizedKey(material, reference)
}

func mediaDefinition(request MediaRequest, needs Requirements, verb reconciliation.Verb) (reconciliation.BlockDefinition, error) {
	canonical, err := request.Canonical()
	if err != nil {
		return reconciliation.BlockDefinition{}, err
	}
	return reconciliation.BlockDefinition{
		ID:             request.Identity.Block,
		Description:    mediaDescription(verb, request),
		Stage:          reconciliation.StageClusters,
		Requires:       mediaRequires(needs),
		Impacts:        mediaImpacts(verb, request),
		Groups:         mediaGroups(verb, request),
		Kind:           Kind,
		Object:         request.Identity.Cluster,
		Implementation: MediaImplementation,
		ContentDigest:  MediaContentDigest(),
		Request:        canonical,
	}, nil
}
