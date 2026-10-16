package ceph

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"maps"
	"slices"
	"strings"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/storage"
)

const ImageImplementation = "storage-ceph-image-v1"
const ImageVersion = "ceph-image-v2"
const imageDeadline = 4 * time.Minute
const maxImageEvidence = 4096

type ImageRequest struct {
	Base        string `json:"base"`
	Cluster     string `json:"cluster"`
	Controller  string `json:"controller"`
	Image       string `json:"image"`
	Registry    string `json:"registry"`
	RegistryRef string `json:"registryRef"`
	Release     string `json:"release"`
	Tag         string `json:"tag"`
	Version     string `json:"version"`
}

func (r ImageRequest) Canonical() ([]byte, error) { return reconciliation.Freeze(r, "Ceph image") }
func DecodeImageRequest(data []byte) (ImageRequest, error) {
	r, err := reconciliation.ThawVersion[ImageRequest](data, "Ceph image", ImageVersion)
	if err != nil {
		return ImageRequest{}, err
	}
	validSelection := storage.NativeVersion(r.Tag) && r.Image == r.Base+":"+r.Tag || r.Tag == "" && strings.HasPrefix(r.Image, r.Base+"@") && ValidImageDigest(strings.TrimPrefix(r.Image, r.Base+"@"))
	if r.Cluster == "" || r.Controller == "" || r.RegistryRef == "" || !(r.Base == r.Registry || strings.HasPrefix(r.Base, r.Registry+"/")) || !storage.NativeVersion(r.Release) || r.Base == "" || !validSelection {
		return ImageRequest{}, failure("lifecycle.state", "the frozen Ceph image request has inconsistent coordinates", "")
	}
	return r, nil
}

func ImageContentDigest() string {
	d := sha256.Sum256([]byte(ImageImplementation + "\x00" + ImageVersion + "\x00" + "selected-artifact-manifest-v2"))
	return hex.EncodeToString(d[:])
}

func ImageBlockID(cluster string) string { return "ceph-image-" + cluster }

type ImageEvidence struct {
	Absent        bool   `json:"absent,omitempty"`
	Base          string `json:"base,omitempty"`
	Digest        string `json:"digest,omitempty"`
	Platform      string `json:"platform,omitempty"`
	Postcondition bool   `json:"postcondition"`
	Release       string `json:"release,omitempty"`
	Request       string `json:"request"`
	Tag           string `json:"tag,omitempty"`
}

func decodeImageEvidence(data []byte, digest string) (ImageEvidence, error) {
	evidence, err := reconciliation.DecodeEvidence[ImageEvidence](data, maxImageEvidence, "Ceph image")
	if err != nil {
		return ImageEvidence{}, err
	}
	if digest == "" || evidence.Request != digest {
		return ImageEvidence{}, failure("lifecycle.state", "Ceph image evidence names another request", "")
	}
	return evidence, nil
}

func imageDigestMismatch() error {
	return failure("lifecycle.unsupported", "the resolved Ceph image does not match the authored digest (image-digest-mismatch)", "select the digest of the intended linux/amd64 manifest")
}

func selectedResolution(e ImageEvidence, r ImageRequest) bool {
	return !e.Absent && e.Postcondition && e.Release == r.Release && e.Base == r.Base && e.Tag == r.Tag && e.Platform == PinnedPlatform && ValidImageDigest(e.Digest)
}

func authoredMatches(e ImageEvidence, r ImageRequest) bool {
	authored, digest := strings.CutPrefix(r.Image, r.Base+"@")
	return !digest || authored == e.Digest
}

func resolution(e ImageEvidence, r ImageRequest) error {
	if !selectedResolution(e, r) {
		return failure("lifecycle.state", "the resolved image is not the linux/amd64 manifest of the selected artifact", "")
	}
	if !authoredMatches(e, r) {
		return imageDigestMismatch()
	}
	return nil
}

func imageEffect(e ImageEvidence, r ImageRequest, removal bool) reconciliation.EffectState {
	resolved := e.Base != "" || e.Digest != "" || e.Platform != "" || e.Release != "" || e.Tag != ""
	switch {
	case e.Absent && (resolved || !e.Postcondition):
		return reconciliation.EffectUnknown
	case e.Absent && removal:
		return reconciliation.EffectCompleted
	case e.Absent:
		return reconciliation.EffectNoEffect
	case resolution(e, r) != nil:
		return reconciliation.EffectUnknown
	case removal:
		return reconciliation.EffectNoEffect
	}
	return reconciliation.EffectCompleted
}

type ImageCapability struct{ runner Runner }

func NewImage(runner Runner) ImageCapability { return ImageCapability{runner: runner} }

func imageDescription(verb reconciliation.Verb, cluster, image string) (string, []string) {
	if verb == reconciliation.Destroy {
		return "release the resolved Ceph image of " + cluster, []string{"release-image " + image}
	}
	return "resolve the selected Ceph image of " + cluster, []string{"resolve-image " + image}
}

func (c ImageCapability) Plan(ctx context.Context, input lifecycle.PlanInput) (lifecycle.CapabilityPlan, error) {
	plan := lifecycle.CapabilityPlan{Definitions: []reconciliation.BlockDefinition{}}
	if err := ctx.Err(); err != nil {
		return plan, err
	}
	if input.State == nil {
		return plan, failure("lifecycle.state", "Ceph planning requires compiled state", "")
	}
	catalog := input.State.Effective()
	for _, object := range catalog.OfKind(api.StorageCluster) {
		r, err := baseRequest(catalog, object, input.Context.Name)
		if err != nil {
			return plan, err
		}
		base, tag := object.Spec().Get("ceph", "image", "base").Text(), object.Spec().Get("ceph", "image", "version").Text()
		if ValidImageDigest(tag) {
			tag = ""
		}
		request := ImageRequest{Version: ImageVersion, Cluster: r.Cluster, Controller: input.Controller, Release: r.Release, Base: base, Tag: tag, Image: r.Image, Registry: r.Registry, RegistryRef: r.RegistryRef}
		canonical, err := request.Canonical()
		if err != nil {
			return plan, err
		}
		description, impacts := imageDescription(input.Verb, r.Cluster, r.Image)
		plan.Definitions = append(plan.Definitions, reconciliation.BlockDefinition{ID: ImageBlockID(r.Cluster), Description: description, Stage: reconciliation.StageClusters, Impacts: impacts, Consumes: []string{},
			Kind: string(api.StorageCluster), Object: r.Cluster, Implementation: ImageImplementation, ContentDigest: ImageContentDigest(), Request: canonical})
		plan.Secrets = append(plan.Secrets, r.RegistryRef)
	}
	slices.Sort(plan.Secrets)
	plan.Secrets = slices.Compact(plan.Secrets)
	return plan, nil
}

func (ImageCapability) Removal(ctx context.Context, block reconciliation.Block) (lifecycle.Removal, error) {
	if err := ctx.Err(); err != nil {
		return lifecycle.Removal{}, err
	}
	r, err := DecodeImageRequest(block.Request)
	if err != nil {
		return lifecycle.Removal{}, err
	}
	description, impacts := imageDescription(reconciliation.Destroy, r.Cluster, r.Image)
	return lifecycle.Removal{Description: description, Impacts: impacts, Consumes: []string{}}, nil
}

func imageRefusals() map[string]error {
	out := map[string]error{"image-digest-mismatch": imageDigestMismatch()}
	for _, reason := range []string{"request", "artifact", "material", "discovery", "timeout", "unknown", "adapter-exception"} {
		out[reason] = failure("lifecycle.unknown", "the selected Ceph image resolution refused its "+reason+" boundary", "inspect the attempt's adapter output, then apply again; resolution changes nothing")
	}
	return out
}

func imageRegistryMaterial(e lifecycle.Execution, name, registry string) (secrets.Material, error) {
	user, err := materialPart(e.Material, name, secrets.UsernamePart)
	if err != nil {
		return secrets.Material{}, err
	}
	defer clear(user)
	password, err := materialPart(e.Material, name, secrets.PasswordPart)
	if err != nil {
		return secrets.Material{}, err
	}
	defer clear(password)
	data, err := json.Marshal(map[string]string{"url": registry, "username": string(user), "password": string(password)})
	if err != nil {
		return secrets.Material{}, err
	}
	defer clear(data)
	return secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: data}), nil
}

func (c ImageCapability) run(ctx context.Context, e lifecycle.Execution, operation string) (lifecycle.RunResult, ImageRequest, error) {
	if err := ctx.Err(); err != nil {
		return lifecycle.RunResult{}, ImageRequest{}, err
	}
	r, err := DecodeImageRequest(e.Block.Request)
	if err != nil {
		return lifecycle.RunResult{}, r, err
	}
	if e.Block.Kind != string(api.StorageCluster) || e.Block.Object != r.Cluster {
		return lifecycle.RunResult{}, r, failure("lifecycle.state", "Ceph image request does not match its frozen resource identity", "")
	}
	if c.runner == nil {
		return lifecycle.RunResult{}, r, failure("lifecycle.state", "the Ceph image adapter is not configured", "")
	}
	canonical, err := r.Canonical()
	if err != nil {
		return lifecycle.RunResult{}, r, err
	}
	var files []lifecycle.MaterialFile
	if operation == "apply" {
		registry, err := imageRegistryMaterial(e, r.RegistryRef, r.Registry)
		if err != nil {
			return lifecycle.RunResult{}, r, err
		}
		defer registry.Clear()
		e.Material = maps.Clone(e.Material)
		e.Material[registryMaterial] = registry
		files = []lifecycle.MaterialFile{{Name: "registry", Secret: registryMaterial, Part: secrets.ValuePart, Variable: "registryJSON"}}
	}
	result, err := c.runner.Run(ctx, lifecycle.RunFor(e, lifecycle.Invocation{Implementation: ImageImplementation, Operation: operation, Variable: "bootwright_ceph_image", Canonical: canonical,
		Placement: machine.Placement{Connection: machine.ConnectionLocal, Machine: r.Controller}, Materials: files, Refusals: imageRefusals(), NativeErrors: true, Deadline: imageDeadline}))
	return result, r, err
}

func (c ImageCapability) Apply(ctx context.Context, e lifecycle.Execution) (lifecycle.Result, error) {
	return c.mutate(ctx, e, "apply")
}
func (c ImageCapability) Destroy(ctx context.Context, e lifecycle.Execution) (lifecycle.Result, error) {
	return c.mutate(ctx, e, "destroy")
}
func (c ImageCapability) mutate(ctx context.Context, e lifecycle.Execution, operation string) (lifecycle.Result, error) {
	result, r, err := c.run(ctx, e, operation)
	if err != nil {
		return lifecycle.Result{Outcome: lifecycle.AttemptOutcome(err)}, err
	}
	outcome := map[string]reconciliation.Outcome{"changed": reconciliation.OutcomeChanged, "unchanged": reconciliation.OutcomeUnchanged}[result.Outcome]
	evidence, err := decodeImageEvidence(result.Evidence, e.Block.RequestDigest)
	if err == nil && operation == "apply" && selectedResolution(evidence, r) && !authoredMatches(evidence, r) {
		return lifecycle.Result{Outcome: reconciliation.OutcomeFailed}, imageDigestMismatch()
	}
	if outcome == "" || err != nil || !evidence.Postcondition || imageEffect(evidence, r, operation == "destroy") != reconciliation.EffectCompleted {
		return lifecycle.Result{Outcome: reconciliation.OutcomeUnknown}, failure("lifecycle.unknown", "the Ceph image resolution did not prove its postcondition", "")
	}
	return lifecycle.Result{Outcome: outcome, Evidence: result.Evidence}, nil
}

func (c ImageCapability) Observe(ctx context.Context, e lifecycle.Execution) (lifecycle.Observation, error) {
	unknown := lifecycle.Observation{Effect: reconciliation.EffectUnknown}
	result, r, err := c.run(ctx, e, "observe")
	if err != nil {
		return unknown, err
	}
	unknown.Evidence = result.Evidence
	evidence, err := decodeImageEvidence(result.Evidence, e.Block.RequestDigest)
	if err != nil {
		return unknown, nil
	}
	return lifecycle.Observation{Effect: imageEffect(evidence, r, false), Evidence: result.Evidence}, nil
}

func (ImageCapability) ObserveRemoval(ctx context.Context, e lifecycle.Execution) (lifecycle.Observation, error) {
	unknown := lifecycle.Observation{Effect: reconciliation.EffectUnknown}
	if err := ctx.Err(); err != nil {
		return unknown, err
	}
	if _, err := DecodeImageRequest(e.Block.Request); err != nil {
		return unknown, err
	}
	evidence, err := json.Marshal(ImageEvidence{Absent: true, Postcondition: true, Request: e.Block.RequestDigest})
	if err != nil {
		return unknown, failure("lifecycle.state", "the Ceph image removal evidence cannot be encoded", "")
	}
	return lifecycle.Observation{Effect: reconciliation.EffectCompleted, Evidence: evidence}, nil
}

func (ImageCapability) Quiescent(ctx context.Context, _ lifecycle.Probe) (lifecycle.Quiescence, error) {
	if err := ctx.Err(); err != nil {
		return lifecycle.Quiescence{State: lifecycle.Unproved}, err
	}
	return lifecycle.Quiescence{State: lifecycle.Quiescent, Reason: "a resolved image reference holds nothing in use"}, nil
}

func (ImageCapability) Unresolved(verb reconciliation.Verb, block reconciliation.Block, data json.RawMessage) (lifecycle.Unresolved, bool) {
	r, err := DecodeImageRequest(block.Request)
	if err != nil {
		return lifecycle.Unresolved{}, false
	}
	evidence, err := decodeImageEvidence(data, block.RequestDigest)
	if err != nil || imageEffect(evidence, r, verb == reconciliation.Destroy) != reconciliation.EffectUnknown {
		return lifecycle.Unresolved{}, false
	}
	refused := resolution(evidence, r)
	if refused == nil {
		refused = failure("lifecycle.state", "the resolution reported a partial or contradictory result", "")
	}
	return lifecycle.Drifted(verb, "the selected Ceph image resolution", "Machine "+r.Controller, refused)
}

func ProvedImage(proofs []lifecycle.BlockEvidence, cluster, authored string) (string, error) {
	found := ""
	for _, proof := range proofs {
		if proof.Kind != string(api.StorageCluster) || proof.Object != cluster || proof.Implementation != ImageImplementation || proof.Verb != reconciliation.Apply || proof.State != reconciliation.BlockDone ||
			proof.Block.Kind != proof.Kind || proof.Block.Object != proof.Object || proof.Block.Implementation != proof.Implementation {
			continue
		}
		if found != "" {
			return "", failure("lifecycle.state", "the Ceph image has ambiguous resolution proofs", "")
		}
		r, err := DecodeImageRequest(proof.Block.Request)
		if err != nil || r.Cluster != cluster {
			return "", failure("lifecycle.state", "the Ceph image resolution proof names another request", "")
		}
		evidence, err := decodeImageEvidence(proof.Evidence, proof.Block.RequestDigest)
		if err != nil {
			return "", failure("lifecycle.state", "the Ceph image resolution proof is unreadable", "")
		}
		if err := resolution(evidence, r); err != nil {
			return "", err
		}
		if r.Image != authored {
			return "", failure("lifecycle.state", "the Ceph image resolution proof was resolved for another authored image", "")
		}
		found = r.Base + "@" + evidence.Digest
	}
	if found == "" {
		return "", failure("lifecycle.state", "Ceph lacks its original apply's image resolution proof", "restore the original apply evidence; the image is resolved once and never again for the same operation")
	}
	return found, nil
}
