package installation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/infrastructureservices/artifactserver"
	machineref "github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
)

const TreeKind = "MachineInstallProfile"
const TreeImplementation = "os-package-tree-v1"
const TreeEvidenceVersion = "os-package-tree-evidence-v1"

type TreeIdentity struct {
	Context string `json:"context"`
	Profile string `json:"profile"`
}

type TreeRequest struct {
	Identity    TreeIdentity         `json:"identity"`
	Media       Media                `json:"media"`
	Placement   machineref.Placement `json:"placement"`
	Publication Publication          `json:"publication"`
	Version     string               `json:"version"`
}

func (r TreeRequest) Canonical() ([]byte, error) { return reconciliation.Freeze(r, "package tree") }

func (r TreeRequest) SecretReferences() []string {
	return lifecycle.SecretNames(append(r.Placement.SecretReferences(), r.Publication.CertificateRef)...)
}
func DecodeTreeRequest(data []byte) (TreeRequest, error) {
	return reconciliation.ThawVersion[TreeRequest](data, "package tree", TreeImplementation)
}
func TreeContentDigest() string {
	digest := sha256.Sum256([]byte(TreeImplementation + "\x00" + TreeEvidenceVersion))
	return hex.EncodeToString(digest[:])
}

type TreeCapability struct {
	runner Runner
	media  MediaRecords
}

func NewTree(runner Runner) TreeCapability                             { return TreeCapability{runner: runner} }
func (c TreeCapability) WithMedia(records MediaRecords) TreeCapability { c.media = records; return c }

func (c TreeCapability) Plan(ctx context.Context, input lifecycle.PlanInput) (lifecycle.CapabilityPlan, error) {
	if err := ctx.Err(); err != nil {
		return lifecycle.CapabilityPlan{}, err
	}
	if input.State == nil {
		return lifecycle.CapabilityPlan{}, refusal("lifecycle.state", "package tree planning requires compiled state", "")
	}
	catalog := input.State.Effective()
	requests, _, err := Requests(catalog, input.Controller, input.Context.Name)
	if err != nil {
		return lifecycle.CapabilityPlan{}, err
	}
	plan := lifecycle.CapabilityPlan{Definitions: []reconciliation.BlockDefinition{}}
	seen := map[string]bool{}
	paths := map[string]Media{}
	var records map[string]MediaRecord
	for _, install := range requests {
		if install.Tree == nil || seen[install.Identity.Profile] {
			continue
		}
		if records == nil {
			if c.media == nil {
				return plan, refusal("lifecycle.state", "package tree planning requires the media store reader", "")
			}
			records, err = c.media.MediaRecords(ctx)
			if err != nil {
				return plan, err
			}
		}
		profile, _ := catalog.Find(api.MachineInstallProfile, install.Identity.Profile)
		source := profile.Spec().Get("installer", "anaconda", "packageSource", "hostedTree")
		server, err := artifactserver.Selected(catalog, source.Get("artifactServerEndpoint"), profile.Identity())
		if err != nil {
			return plan, err
		}
		placement, err := artifactserver.PlacementFor(catalog, server, input.Controller)
		if err != nil {
			return plan, err
		}
		media, err := pinned(*install.TreeMedia, records, profile.Identity(), "spec.installer.anaconda.packageSource.hostedTree.fromMedia", "")
		if err == nil {
			err = refusedRelease(media.Name, records, profile)
		}
		if err != nil {
			return plan, err
		}
		request := TreeRequest{Identity: TreeIdentity{Context: input.Context.Name, Profile: profile.Name()}, Media: media, Placement: placement, Publication: *install.Tree, Version: TreeImplementation}
		if other, exists := paths[request.Publication.Path]; exists && other != media {
			return plan, refusal("lifecycle.state", "one package tree path names conflicting source pins", "select one source per publication path")
		}
		paths[request.Publication.Path], seen[profile.Name()] = media, true
		if err := appendTreePlan(&plan, catalog, input, request, server.Name()); err != nil {
			return plan, err
		}
	}
	slices.Sort(plan.Secrets)
	plan.Secrets = slices.Compact(plan.Secrets)
	return plan, nil
}

func appendTreePlan(plan *lifecycle.CapabilityPlan, catalog api.Catalog, input lifecycle.PlanInput, request TreeRequest, server string) error {
	canonical, err := request.Canonical()
	if err != nil {
		return err
	}
	environments := catalog.OfKind(api.Environment)
	if len(environments) != 1 {
		return refusal("lifecycle.state", "package tree publication requires one Environment", "")
	}
	plan.Definitions = append(plan.Definitions, reconciliation.BlockDefinition{
		ID: reconciliation.BlockIdentity("os-package-tree-", request.Identity.Profile), Description: treeDescription(input.Verb), Stage: reconciliation.StageMachines,
		Requires:  []reconciliation.ObjectRef{{Kind: "ArtifactServer", Object: server}, {Kind: "Environment", Object: environments[0].Name()}},
		Exclusive: []string{"path:" + request.Publication.Path}, Kind: TreeKind, Object: request.Identity.Profile,
		Implementation: TreeImplementation, ContentDigest: TreeContentDigest(), Request: canonical,
		Impacts: treeImpacts(input.Verb, request),
	})
	plan.Secrets = append(plan.Secrets, request.Placement.SecretReferences()...)
	if request.Publication.CertificateRef != "" {
		plan.Secrets = append(plan.Secrets, request.Publication.CertificateRef)
	}
	if request.Placement.Local() {
		plan.Reservations = append(plan.Reservations, prerequisites.HostReservation{
			Context: input.Context.Name, Kind: "os-package-tree", Service: request.Identity.Profile, Keys: []string{"path:" + request.Publication.Path},
		})
	}
	return nil
}

func treeDescription(verb reconciliation.Verb) string {
	if verb == reconciliation.Destroy {
		return "remove the shared installation package tree"
	}
	return "publish the shared installation package tree"
}
func treeImpacts(verb reconciliation.Verb, request TreeRequest) []string {
	action := "publish-content "
	if verb == reconciliation.Destroy {
		action = "remove-content "
	}
	return []string{action + request.Publication.Path}
}
func (TreeCapability) Removal(ctx context.Context, block reconciliation.Block) (lifecycle.Removal, error) {
	if err := ctx.Err(); err != nil {
		return lifecycle.Removal{}, err
	}
	request, err := DecodeTreeRequest(block.Request)
	if err != nil {
		return lifecycle.Removal{}, err
	}
	return lifecycle.Removal{Description: treeDescription(reconciliation.Destroy), Impacts: treeImpacts(reconciliation.Destroy, request)}, nil
}
func (c TreeCapability) run(ctx context.Context, execution lifecycle.Execution, operation string) (lifecycle.RunResult, TreeRequest, error) {
	if err := ctx.Err(); err != nil {
		return lifecycle.RunResult{}, TreeRequest{}, err
	}
	request, err := DecodeTreeRequest(execution.Block.Request)
	if err != nil {
		return lifecycle.RunResult{}, request, err
	}
	if c.runner == nil {
		return lifecycle.RunResult{}, request, refusal("lifecycle.state", "the package tree adapter is not configured", "")
	}
	canonical, err := request.Canonical()
	if err != nil {
		return lifecycle.RunResult{}, request, err
	}
	var materials []lifecycle.MaterialFile
	var refusals map[string]error
	if operation == "apply" {
		refusals = treeRefusals(execution.Context, request)
		if request.Publication.CertificateRef != "" {
			materials = append(materials, lifecycle.MaterialFile{Name: "tree-ca", Secret: request.Publication.CertificateRef, Part: secrets.CertificatePart, Variable: "certificate"})
		}
	}
	result, err := c.runner.Run(ctx, lifecycle.RunFor(execution, lifecycle.Invocation{Implementation: TreeImplementation,
		Operation: operation, Variable: "bootwright_os_tree", Canonical: canonical, Placement: request.Placement,
		Materials: materials, Refusals: refusals, Deadline: time.Hour,
	}))
	return result, request, err
}

// treeRefusals are the diagnostics an apply names before it extracts anything:
// a source image that no longer has the size and SHA-256 the operation froze,
// and a tree already published at the path from another image.
func treeRefusals(contextName string, request TreeRequest) map[string]error {
	profile := diagnostics.ObjectIdentity{APIVersion: api.APIVersion, Kind: string(api.MachineInstallProfile), Name: request.Identity.Profile}
	return map[string]error{
		"media-changed-tree": changedMedia(contextName, request.Media.Name, profile),
		"tree-source-drift": &diagnostics.Failure{Diagnostics: []diagnostics.Diagnostic{{
			Severity: "error", Code: "lifecycle.state",
			Message: "the package tree at " + request.Publication.Path + " was extracted from another image than the " + request.Media.Name +
				" this operation froze, so it was left unchanged and nothing was published",
			Remediation: "take this context back with bootwright destroy --context " + contextName +
				", which removes that tree, then run bootwright apply --context " + contextName,
			Object: &profile,
		}}},
	}
}
func (c TreeCapability) Apply(ctx context.Context, execution lifecycle.Execution) (lifecycle.Result, error) {
	return c.mutate(ctx, execution, "apply")
}
func (c TreeCapability) Destroy(ctx context.Context, execution lifecycle.Execution) (lifecycle.Result, error) {
	return c.mutate(ctx, execution, "destroy")
}
func (c TreeCapability) mutate(ctx context.Context, execution lifecycle.Execution, operation string) (lifecycle.Result, error) {
	result, request, err := c.run(ctx, execution, operation)
	if err != nil {
		return lifecycle.Result{Outcome: lifecycle.AttemptOutcome(err)}, err
	}
	outcome := reconciliation.OutcomeUnknown
	if result.Outcome == "changed" {
		outcome = reconciliation.OutcomeChanged
	}
	if result.Outcome == "unchanged" {
		outcome = reconciliation.OutcomeUnchanged
	}
	evidence, err := decodeTreeEvidence(result.Evidence, execution.Block.RequestDigest)
	valid := err == nil && evidence.Postcondition && (operation != "destroy" || evidence.Absent) && treeEffect(evidence, request, operation == "destroy") == reconciliation.EffectCompleted
	if outcome == reconciliation.OutcomeUnknown || !valid {
		return lifecycle.Result{Outcome: reconciliation.OutcomeUnknown}, refusal("lifecycle.state", "the package tree adapter did not prove its postcondition", "")
	}
	return lifecycle.Result{Outcome: outcome, Evidence: result.Evidence}, nil
}
func (c TreeCapability) Observe(ctx context.Context, execution lifecycle.Execution) (lifecycle.Observation, error) {
	return c.observe(ctx, execution, false)
}
func (c TreeCapability) ObserveRemoval(ctx context.Context, execution lifecycle.Execution) (lifecycle.Observation, error) {
	return c.observe(ctx, execution, true)
}
func (c TreeCapability) observe(ctx context.Context, execution lifecycle.Execution, removal bool) (lifecycle.Observation, error) {
	observation := lifecycle.Observation{Effect: reconciliation.EffectUnknown}
	result, request, err := c.run(ctx, execution, "observe")
	if err != nil {
		return observation, err
	}
	observation.Evidence = result.Evidence
	evidence, err := decodeTreeEvidence(result.Evidence, execution.Block.RequestDigest)
	if err != nil {
		return observation, nil
	}
	observation.Effect = treeEffect(evidence, request, removal)
	return observation, nil
}
func (TreeCapability) Quiescent(context.Context, lifecycle.Probe) (lifecycle.Quiescence, error) {
	return lifecycle.Quiescence{State: lifecycle.Quiescent, Reason: "installation consumers precede their package tree in removal"}, nil
}
func (TreeCapability) Unresolved(verb reconciliation.Verb, block reconciliation.Block, data json.RawMessage) (lifecycle.Unresolved, bool) {
	request, err := DecodeTreeRequest(block.Request)
	if err != nil {
		return lifecycle.Unresolved{}, false
	}
	evidence, err := decodeTreeEvidence(data, block.RequestDigest)
	if err != nil || treeEffect(evidence, request, verb == reconciliation.Destroy) != reconciliation.EffectUnknown {
		return lifecycle.Unresolved{}, false
	}
	return lifecycle.Drifted(verb, "the shared package tree", lifecycle.PlacedOn(request.Placement), refusal("lifecycle.state", "the tree identity differs from the frozen media source", ""))
}
