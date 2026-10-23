package installation

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/canonicaljson"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
)

type EraseCapability struct {
	runner             Runner
	identities         Identities
	media              MediaRecords
	claim              func(string) string
	retirementPolicies func(api.Catalog, string) ([]string, error)
}

func NewErase(runner Runner) EraseCapability { return EraseCapability{runner: runner} }

func (c EraseCapability) WithClaims(reader func(string) string) EraseCapability {
	c.claim = reader
	return c
}

func (c EraseCapability) WithRetirementPolicies(reader func(api.Catalog, string) ([]string, error)) EraseCapability {
	c.retirementPolicies = reader
	return c
}

func (c EraseCapability) WithIdentities(reader Identities) EraseCapability {
	c.identities = reader
	return c
}

func (c EraseCapability) WithMedia(reader MediaRecords) EraseCapability {
	c.media = reader
	return c
}

func (c EraseCapability) Plan(ctx context.Context, input lifecycle.PlanInput) (lifecycle.CapabilityPlan, error) {
	plan := lifecycle.CapabilityPlan{Definitions: []reconciliation.BlockDefinition{}}
	if input.State == nil {
		return plan, refusal("lifecycle.state", "physical erase planning requires compiled state", "")
	}
	catalog := input.State.Effective()
	selected := map[string]api.Object{}
	for _, machine := range catalog.Objects() {
		if machine.Kind() == api.Machine && machine.Spec().Has("hardware", "eraseOnRemoval") {
			selected[machine.Name()] = machine
		}
	}
	if len(selected) == 0 {
		return plan, nil
	}
	installs, requirements, err := Requests(catalog, input.Controller, input.Context.Name)
	if err != nil {
		return plan, err
	}
	if c.media == nil || c.claim == nil {
		return plan, refusal("lifecycle.state", "physical erase requires media and original claim readers", "")
	}
	records, err := c.media.MediaRecords(ctx)
	if err != nil {
		return plan, err
	}
	for index, install := range installs {
		machine, found := selected[install.Identity.Object]
		if !found {
			continue
		}
		delete(selected, install.Identity.Object)
		install, err = pinMedia(catalog, install, records)
		if err != nil {
			return plan, err
		}
		request, err := eraseRequest(machine, install)
		if err != nil {
			return plan, err
		}
		canonical, err := request.Canonical()
		if err != nil {
			return plan, err
		}
		needs := requires(requirements[index])
		needs = slices.DeleteFunc(needs, func(ref reconciliation.ObjectRef) bool { return ref.Kind == "Machine" })
		definition := reconciliation.BlockDefinition{ID: EraseBlockID(machine.Name()), Description: "retain the verified physical erase policy of " + machine.Name(), Stage: reconciliation.StageMachines,
			Requires: needs, Dependencies: []string{c.claim(machine.Name())}, Kind: Kind, Object: machine.Name(), Implementation: EraseImplementation, ContentDigest: EraseContentDigest(), Request: canonical, Exclusive: install.ExclusiveKeys()}
		if c.retirementPolicies != nil {
			policies, err := c.retirementPolicies(catalog, machine.Name())
			if err != nil {
				return plan, err
			}
			definition.Dependencies = append(definition.Dependencies, policies...)
			slices.Sort(definition.Dependencies)
			definition.Dependencies = slices.Compact(definition.Dependencies)
		}
		if input.Verb == reconciliation.Destroy {
			definition.Description = "overwrite and fully read back the declared physical disks of " + machine.Name()
			definition.Consumes = []string{reconciliation.AuthorizationDataLoss}
			definition.Impacts = []string{"boot-offline-erase " + machine.Name()}
			for _, target := range request.Targets {
				definition.Impacts = append(definition.Impacts, "logical-overwrite "+target.DeviceName)
			}
		}
		plan.Definitions = append(plan.Definitions, definition)
		plan.Secrets = append(plan.Secrets, install.SecretReferences()...)
	}
	if len(selected) > 0 {
		return plan, refusal("api.invariant", "eraseOnRemoval requires a Bootwright-installed physical Machine", "omit eraseOnRemoval on provided or virtual Machines")
	}
	slices.Sort(plan.Secrets)
	plan.Secrets = slices.Compact(plan.Secrets)
	return plan, nil
}

func (c EraseCapability) Removal(ctx context.Context, block reconciliation.Block) (lifecycle.Removal, error) {
	request, err := DecodeEraseRequest(block.Request)
	if err != nil {
		return lifecycle.Removal{}, err
	}
	impacts := []string{"boot-offline-erase " + request.Install.Identity.Object}
	for _, target := range request.Targets {
		impacts = append(impacts, "logical-overwrite "+target.DeviceName)
	}
	return lifecycle.Removal{Description: "overwrite and fully read back the declared physical disks of " + request.Install.Identity.Object, Impacts: impacts, Consumes: []string{reconciliation.AuthorizationDataLoss}}, nil
}

func (c EraseCapability) Apply(ctx context.Context, execution lifecycle.Execution) (lifecycle.Result, error) {
	request, err := DecodeEraseRequest(execution.Block.Request)
	if err != nil {
		return lifecycle.Result{}, err
	}
	if _, err = c.identity(execution, request); err != nil {
		return lifecycle.Result{}, err
	}
	proof, _ := json.Marshal(EraseEvidence{Version: EraseImplementation, Request: execution.Block.RequestDigest, Prepared: true})
	return lifecycle.Result{Outcome: reconciliation.OutcomeUnchanged, Evidence: proof}, nil
}

func (c EraseCapability) Observe(ctx context.Context, execution lifecycle.Execution) (lifecycle.Observation, error) {
	result, err := c.Apply(ctx, execution)
	if err != nil {
		return lifecycle.Observation{Effect: reconciliation.EffectUnknown}, err
	}
	return lifecycle.Observation{Effect: reconciliation.EffectCompleted, Evidence: result.Evidence}, nil
}

func (c EraseCapability) Destroy(ctx context.Context, execution lifecycle.Execution) (lifecycle.Result, error) {
	request, err := DecodeEraseRequest(execution.Block.Request)
	if err != nil {
		return lifecycle.Result{}, err
	}
	needed, err := installationStarted(execution, request)
	if err != nil {
		return lifecycle.Result{}, err
	}
	if !needed {
		proof, _ := json.Marshal(EraseEvidence{Version: EraseImplementation, Request: execution.Block.RequestDigest, Prepared: true, Removed: true, Retained: true})
		return lifecycle.Result{Outcome: reconciliation.OutcomeUnchanged, Evidence: proof}, nil
	}
	result, err := c.run(ctx, execution, request, "destroy")
	if err != nil {
		return lifecycle.Result{Outcome: lifecycle.AttemptOutcome(err)}, err
	}
	if err = eraseEvidence(result.Evidence, request, execution.Block.RequestDigest, true); err != nil {
		return lifecycle.Result{Outcome: reconciliation.OutcomeUnknown}, err
	}
	return lifecycle.Result{Outcome: reconciliation.OutcomeChanged, Evidence: result.Evidence}, nil
}

func (c EraseCapability) ObserveRemoval(ctx context.Context, execution lifecycle.Execution) (lifecycle.Observation, error) {
	request, err := DecodeEraseRequest(execution.Block.Request)
	if err != nil {
		return lifecycle.Observation{Effect: reconciliation.EffectUnknown}, err
	}
	needed, err := installationStarted(execution, request)
	if err != nil {
		return lifecycle.Observation{Effect: reconciliation.EffectUnknown}, err
	}
	if !needed {
		proof, _ := json.Marshal(EraseEvidence{Version: EraseImplementation, Request: execution.Block.RequestDigest, Prepared: true, Removed: true, Retained: true})
		return lifecycle.Observation{Effect: reconciliation.EffectCompleted, Evidence: proof}, nil
	}
	result, err := c.run(ctx, execution, request, "observe")
	if err != nil {
		return lifecycle.Observation{Effect: reconciliation.EffectUnknown}, err
	}
	effect := reconciliation.EffectPartial
	if eraseEvidence(result.Evidence, request, execution.Block.RequestDigest, true) == nil {
		effect = reconciliation.EffectCompleted
	}
	return lifecycle.Observation{Effect: effect, Evidence: result.Evidence}, nil
}

func installationStarted(execution lifecycle.Execution, request EraseRequest) (bool, error) {
	for _, original := range execution.OriginalRemovalDependents {
		if original.Block.ID != BlockID(request.Install.Identity.Object) {
			continue
		}
		if !originalEraseBlock(original, request.Install.Identity.Object, Implementation, BlockID(request.Install.Identity.Object)) || original.Block.RequestDigest != request.InstallDigest {
			break
		}
		if original.State == reconciliation.BlockPending && len(original.Evidence) == 0 {
			return false, nil
		}
		return true, nil
	}
	return false, refusal("lifecycle.state", "physical erase has no original installation effect boundary", "restore the original apply records before continuing removal")
}

func (EraseCapability) Quiescent(context.Context, lifecycle.Probe) (lifecycle.Quiescence, error) {
	return lifecycle.Quiescence{State: lifecycle.Quiescent, Reason: "offline erase independently requires a powered-off target or its authenticated RAM worker"}, nil
}

func (EraseCapability) RemovesHost(block reconciliation.Block) bool {
	if block.Kind != Kind || block.Implementation != EraseImplementation || block.ID != EraseBlockID(block.Object) {
		return false
	}
	request, err := DecodeEraseRequest(block.Request)
	return err == nil && request.Install.Identity.Object == block.Object && request.Install.Target.Physical && len(request.Targets) > 0
}

func (c EraseCapability) identity(execution lifecycle.Execution, request EraseRequest) (map[string]string, error) {
	if c.identities == nil {
		return nil, refusal("lifecycle.state", "physical erase has no original machine identity reader", "")
	}
	proofs := execution.ApplyProved
	if len(proofs) == 0 {
		proofs = execution.Proved
	}
	pin, found, err := c.identities.PinnedIdentity(request.Install.Identity.Object, proofs)
	if err != nil {
		return nil, err
	}
	if !found || pin.UUID == "" {
		return nil, refusal("lifecycle.state", "physical erase requires the original system UUID proof", "restore the original apply records")
	}
	return pin.PinValues(""), nil
}

func (c EraseCapability) run(ctx context.Context, execution lifecycle.Execution, request EraseRequest, operation string) (lifecycle.RunResult, error) {
	if c.runner == nil {
		return lifecycle.RunResult{}, refusal("lifecycle.state", "physical erase runner is missing", "")
	}
	if operation == "destroy" && execution.PrepareRemoval == nil {
		return lifecycle.RunResult{}, refusal("lifecycle.state", "physical erase requires durable removal receipt custody before any effect", "continue the original destroy through the lifecycle engine")
	}
	values, err := c.identity(execution, request)
	if err != nil {
		return lifecycle.RunResult{}, err
	}
	install := request.Install
	key, err := authorizedKey(execution, install)
	if err != nil {
		return lifecycle.RunResult{}, err
	}
	values["authorizedKey"] = key
	values["originalAttempt"] = fmt.Sprintf("%s/%s/%d", execution.Operation, execution.Block.ID, execution.Attempt)
	values["applyOperation"] = execution.ApplyOperation
	if values["applyOperation"] == "" {
		return lifecycle.RunResult{}, refusal("lifecycle.state", "physical erase has no original apply operation", "")
	}
	materials := []lifecycle.MaterialFile{
		{Name: "fleet-id", Part: secrets.PrivateKeyPart, Secret: install.FleetKeyRef, Variable: "fleetIdentity"},
		{Name: "bmc-user", Part: secrets.UsernamePart, Secret: install.Target.Controller.CredentialsRef, Variable: "controllerUser"},
		{Name: "bmc-password", Part: secrets.PasswordPart, Secret: install.Target.Controller.CredentialsRef, Variable: "controllerPassword"},
		{Name: "artifact-ca", Part: secrets.CertificatePart, Secret: install.TLSCertificateRef, Variable: "artifactCertificate"},
	}
	if install.Target.Controller.TrustBundleRef != "" {
		materials = append(materials, lifecycle.MaterialFile{Name: "bmc-ca", Part: secrets.CertificatePart, Secret: install.Target.Controller.TrustBundleRef, Variable: "controllerCA"})
	}
	canonical, err := request.Canonical()
	if err != nil {
		return lifecycle.RunResult{}, err
	}
	deadline := time.Duration(request.DeadlineSeconds)*time.Second + 2*time.Hour
	if operation == "observe" {
		deadline = 3 * time.Minute
	}
	validate := func(data json.RawMessage) error {
		return eraseLineage(data, request, execution, values["pinnedUUIDBase64"])
	}
	result, err := c.runner.Run(ctx, lifecycle.RunFor(execution, lifecycle.Invocation{Implementation: EraseImplementation, Operation: operation, Variable: eraseVariable, Canonical: canonical,
		Placement: install.Placement, Materials: materials, Values: values, Deadline: deadline, RemovalPreparation: validate}))
	if err != nil {
		return result, err
	}
	proof, err := reconciliation.DecodeEvidence[EraseEvidence](result.Evidence, 32768, "physical erase")
	if err != nil {
		return lifecycle.RunResult{}, err
	}
	if proof.Receipt != nil {
		data, _ := json.Marshal(proof.Receipt)
		if err := validate(data); err != nil {
			return lifecycle.RunResult{}, err
		}
	}
	return result, nil
}

func eraseLineage(data json.RawMessage, request EraseRequest, execution lifecycle.Execution, encodedUUID string) error {
	proof, err := reconciliation.DecodeEvidence[EraseReceipt](data, 32768, "physical erase receipt")
	if err != nil {
		return err
	}
	uuid, err := base64.StdEncoding.DecodeString(encodedUUID)
	if err != nil || len(uuid) == 0 || !strings.EqualFold(strings.TrimSpace(proof.UUID), strings.TrimSpace(string(uuid))) {
		return refusal("lifecycle.state", "physical erase receipt has another original machine UUID", "restore the original apply and removal records")
	}
	prefix := execution.Operation + "/" + execution.Block.ID + "/"
	number, found := strings.CutPrefix(proof.OriginalAttempt, prefix)
	attempt, parsed := strconv.Atoi(number)
	// Attempt starts are durable and consecutive within this exact removal.
	if !found || parsed != nil || attempt < 1 || attempt > execution.Attempt || strconv.Itoa(attempt) != number {
		return refusal("lifecycle.state", "physical erase receipt has another removal attempt lineage", "continue the original destroy operation")
	}
	if err := validateEraseReceipt(&proof, request, execution.Block.RequestDigest); err != nil {
		return err
	}
	canonical, _ := canonicaljson.Encode(proof, canonicaljson.Bare)
	for _, previous := range execution.RemovalPreparations {
		stored, err := reconciliation.DecodeEvidence[EraseReceipt](previous, 32768, "physical erase receipt")
		if err != nil {
			return err
		}
		if canonicaljson.Prove(canonical, stored, canonicaljson.Bare) != nil {
			return refusal("lifecycle.state", "physical erase receipt differs from its durable removal preparation", "restore the original removal records")
		}
	}
	return nil
}
