package physicalerase

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
	"github.com/crmarques/bootwright/internal/infrastructureservices"
	"github.com/crmarques/bootwright/internal/managedos/installation"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/substrate"
)

type Capability struct {
	runner             Runner
	identities         installation.Identities
	media              installation.MediaRecords
	retirementPolicies []installation.RetirementPolicy
	discoveredRoots    installation.DiscoveredRoots
}

func New(runner Runner) Capability { return Capability{runner: runner} }

func (c Capability) WithRetirementPolicies(policies ...installation.RetirementPolicy) Capability {
	c.retirementPolicies = slices.Clone(policies)
	return c
}

func (c Capability) WithIdentities(reader installation.Identities) Capability {
	c.identities = reader
	return c
}

func (c Capability) WithMedia(reader installation.MediaRecords) Capability {
	c.media = reader
	return c
}

func (c Capability) Plan(ctx context.Context, input lifecycle.PlanInput) (lifecycle.CapabilityPlan, error) {
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
	installs, requirements, err := installation.ResolvedRequests(ctx, input, c.discoveredRoots)
	if err != nil {
		return plan, err
	}
	if c.media == nil {
		return plan, refusal("lifecycle.state", "physical erase requires a media reader", "")
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
		install, err = installation.PinMedia(catalog, install, records)
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
		needs := installation.RequiredObjects(requirements[index])
		needs = slices.DeleteFunc(needs, func(ref reconciliation.ObjectRef) bool { return ref.Kind == "Machine" })
		needs = append(needs, reconciliation.ObjectRef{Kind: "Machine", Object: machine.Name(), Family: substrate.MachineFamily})
		retirement, err := installation.RetirementRequirements(c.retirementPolicies, catalog, machine.Name())
		if err != nil {
			return plan, err
		}
		definition := reconciliation.BlockDefinition{ID: EraseBlockID(machine.Name()), Description: "retain the verified physical erase policy of " + machine.Name(), Stage: reconciliation.StageMachines,
			Requires: append(needs, retirement...), Family: installation.EraseFamily, Kind: installation.Kind, Object: machine.Name(), Implementation: EraseImplementation, ContentDigest: EraseContentDigest(), Request: canonical, Exclusive: install.ExclusiveKeys()}
		if input.Verb == reconciliation.Destroy {
			definition.Description = eraseDescription + machine.Name()
			definition.Consumes = []string{reconciliation.AuthorizationDataLoss}
			definition.Impacts = eraseImpacts(request)
			definition.Groups = eraseGroups(request)
		}
		plan.Definitions = append(plan.Definitions, definition)
		plan.Secrets = append(plan.Secrets, install.SecretReferences()...)
		plan.Reservations = append(plan.Reservations, infrastructureservices.HostReservation{
			Context: input.Context.Name, Kind: installation.EraseFamily, Service: machine.Name(), ObjectKind: installation.Kind,
			Keys: []string{infrastructureservices.ControllerMediaReservationKey(install.Target.Controller.Endpoint).String()},
		})
	}
	if len(selected) > 0 {
		return plan, refusal("api.invariant", "eraseOnRemoval requires a Bootwright-installed physical Machine", "omit eraseOnRemoval on provided or virtual Machines")
	}
	slices.Sort(plan.Secrets)
	plan.Secrets = slices.Compact(plan.Secrets)
	return plan, nil
}

func (c Capability) Removal(ctx context.Context, block reconciliation.Block) (lifecycle.Removal, error) {
	request, err := DecodeEraseRequest(block.Request)
	if err != nil {
		return lifecycle.Removal{}, err
	}
	return eraseRemoval(request, ""), nil
}

func eraseGroups(request EraseRequest) []reconciliation.Group {
	machines := []string{request.Install.Identity.Object}
	groups := make([]reconciliation.Group, 0, len(request.Targets))
	for index, target := range request.Targets {
		description := "zero and read back " + eraseTargetName(target)
		if target.Method == EraseMetadataClear {
			description = "clear the metadata of " + eraseTargetName(target)
		}
		groups = append(groups, reconciliation.Group{ID: "erase-" + strconv.Itoa(index+1), Description: description, Machines: machines})
	}
	return groups
}

const eraseDescription = "overwrite and read back the declared physical disks of "

func eraseImpacts(request EraseRequest) []string { return removalImpacts(request, "") }

func removalImpacts(request EraseRequest, relaxation string) []string {
	if relaxation == lifecycle.EraseRetain {
		return relaxedImpacts(request, relaxation)
	}
	impacts := []string{"boot-offline-erase " + request.Install.Identity.Object}
	if relaxation != "" {
		impacts = append(impacts, relaxedImpacts(request, relaxation)...)
	}
	if request.Install.Target.Controller.VirtualMedia.EnableVerification {
		impacts = append(impacts, "enable-bmc-media-verification "+request.Install.Identity.Object+" (BMC-wide; retained)")
	}
	if request.Install.Target.Controller.VirtualMedia.Trust == substrate.TrustDisableVerification {
		impacts = append(impacts, "fetch-private-media-without-certificate-verification "+request.Install.Identity.Object)
		if request.Install.Target.Controller.VirtualMedia.RestoreVerification {
			impacts = append(impacts, "restore-bmc-media-verification-after-boot "+request.Install.Identity.Object)
		}
	}
	for _, target := range request.Targets {
		impact := "logical-overwrite " + eraseTargetName(target)
		switch {
		case target.Method == EraseMetadataClear:
			impact = "metadata-clear " + eraseTargetName(target)
		case relaxation == lifecycle.EraseMetadataClear:
			impact = "metadata-clear " + eraseTargetName(target) + " (relaxed from logical-overwrite)"
		}
		impacts = append(impacts, impact)
	}
	return impacts
}

func (c Capability) Apply(ctx context.Context, execution lifecycle.Execution) (lifecycle.Result, error) {
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

func (c Capability) Observe(ctx context.Context, execution lifecycle.Execution) (lifecycle.Observation, error) {
	result, err := c.Apply(ctx, execution)
	if err != nil {
		return lifecycle.Observation{Effect: reconciliation.EffectUnknown}, err
	}
	return lifecycle.Observation{Effect: reconciliation.EffectCompleted, Evidence: result.Evidence}, nil
}

func (c Capability) Destroy(ctx context.Context, execution lifecycle.Execution) (lifecycle.Result, error) {
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
	if execution.Block.Relaxation == lifecycle.EraseRetain {
		return c.destroyWaived(ctx, execution, request)
	}
	effective, err := relaxedRequest(request, execution.Block.Relaxation)
	if err != nil {
		return lifecycle.Result{}, err
	}
	result, err := c.run(ctx, execution, request, effective, "destroy")
	if err != nil {
		return lifecycle.Result{Outcome: lifecycle.AttemptOutcome(err)}, err
	}
	if err = eraseEvidence(result.Evidence, effective, execution.Block.RequestDigest, true); err != nil {
		return lifecycle.Result{Outcome: reconciliation.OutcomeUnknown}, err
	}
	return lifecycle.Result{Outcome: reconciliation.OutcomeChanged, Evidence: result.Evidence}, nil
}

func (c Capability) ObserveRemoval(ctx context.Context, execution lifecycle.Execution) (lifecycle.Observation, error) {
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
	if execution.Block.Relaxation == lifecycle.EraseRetain {
		return c.observeWaived(ctx, execution, request)
	}
	effective, err := relaxedRequest(request, execution.Block.Relaxation)
	if err != nil {
		return lifecycle.Observation{Effect: reconciliation.EffectUnknown}, err
	}
	result, err := c.run(ctx, execution, request, effective, "observe")
	if err != nil {
		return lifecycle.Observation{Effect: reconciliation.EffectUnknown}, err
	}
	effect := reconciliation.EffectUnknown
	if eraseEvidence(result.Evidence, effective, execution.Block.RequestDigest, true) == nil {
		effect = reconciliation.EffectCompleted
	} else if eraseUnfinished(result.Evidence, execution.Block.RequestDigest) {
		effect = reconciliation.EffectPartial
	}
	return lifecycle.Observation{Effect: effect, Evidence: result.Evidence}, nil
}

func eraseUnfinished(data json.RawMessage, digest string) bool {
	proof, err := reconciliation.DecodeEvidence[EraseEvidence](data, 32768, "physical erase")
	return err == nil && proof.Version == EraseImplementation && proof.Request == digest && !proof.Removed && !proof.Retained
}

func installationStarted(execution lifecycle.Execution, request EraseRequest) (bool, error) {
	for _, original := range execution.OriginalRemovalDependents {
		if original.Block.ID != installation.BlockID(request.Install.Identity.Object) {
			continue
		}
		if !installation.OriginalInstall(original, request.Install.Identity.Object) || original.Block.RequestDigest != request.InstallDigest {
			break
		}
		if original.State == reconciliation.BlockPending && len(original.Evidence) == 0 {
			return false, nil
		}
		return true, nil
	}
	return false, refusal("lifecycle.state", "physical erase has no original installation effect boundary", "restore the original apply records before continuing removal")
}

func installedRootWWN(execution lifecycle.Execution, request EraseRequest) (string, error) {
	install := request.Install.Identity.Object
	for _, original := range execution.OriginalRemovalDependents {
		if original.Block.ID != installation.BlockID(install) {
			continue
		}
		if !installation.OriginalInstall(original, install) || original.Block.RequestDigest != request.InstallDigest {
			break
		}
		return installation.RecordedRootWWN(original, request.Install, request.InstallDigest)
	}
	return "", refusal("lifecycle.state", "physical erase has no original installation effect boundary", "restore the original apply records before continuing removal")
}

func (Capability) Quiescent(context.Context, lifecycle.Probe) (lifecycle.Quiescence, error) {
	return lifecycle.Quiescence{State: lifecycle.Quiescent, Reason: "offline erase independently requires a powered-off target or its authenticated RAM worker"}, nil
}

func (Capability) RemovesHost(block reconciliation.Block) bool {
	if block.Kind != installation.Kind || block.Implementation != EraseImplementation || block.ID != EraseBlockID(block.Object) {
		return false
	}
	request, err := DecodeEraseRequest(block.Request)
	return err == nil && request.Install.Identity.Object == block.Object && request.Install.Target.Physical && len(request.Targets) > 0
}

func (c Capability) identity(execution lifecycle.Execution, request EraseRequest) (map[string]string, error) {
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

func (c Capability) run(ctx context.Context, execution lifecycle.Execution, request, effective EraseRequest, operation string) (lifecycle.RunResult, error) {
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
	if install.Target.Hardware == nil {
		return lifecycle.RunResult{}, refusal("lifecycle.state", "physical erase has no frozen physical hardware", "restore the original apply records")
	}
	recorded, err := installedRootWWN(execution, request)
	if err != nil {
		return lifecycle.RunResult{}, err
	}
	if err := eraseRootRules(request.Targets, install.Target.Hardware.RootDevice, install.Target.Hardware, recorded); err != nil {
		return lifecycle.RunResult{}, err
	}
	rootWWN := install.Target.Hardware.WWN
	if rootWWN == "" {
		rootWWN = recorded
	}
	values["rootWWNBase64"] = base64.StdEncoding.EncodeToString([]byte(rootWWN))
	key, err := installation.AuthorizedKey(execution, install)
	if err != nil {
		return lifecycle.RunResult{}, err
	}
	values["authorizedKey"] = key
	values["originalAttempt"] = fmt.Sprintf("%s/%s/%d", execution.Operation, execution.Block.ID, execution.Attempt)
	values["applyOperation"] = execution.ApplyOperation
	if execution.Block.Relaxation != "" {
		values["relaxationBase64"] = base64.StdEncoding.EncodeToString([]byte(execution.Block.Relaxation))
	}
	if values["applyOperation"] == "" {
		return lifecycle.RunResult{}, refusal("lifecycle.state", "physical erase has no original apply operation", "")
	}
	port := install.Target.MachinePort
	if port.UserVariable == "" || port.PasswordVariable == "" {
		return lifecycle.RunResult{}, refusal("lifecycle.state", "the frozen machine port names no controller account material", "")
	}
	materials := []lifecycle.MaterialFile{
		{Name: "fleet-id", Part: secrets.PrivateKeyPart, Secret: install.FleetKeyRef, Variable: "fleetIdentity"},
		{Name: "bmc-user", Part: secrets.UsernamePart, Secret: install.Target.Controller.CredentialsRef, Variable: port.UserVariable},
		{Name: "bmc-password", Part: secrets.PasswordPart, Secret: install.Target.Controller.CredentialsRef, Variable: port.PasswordVariable},
		{Name: "artifact-ca", Part: secrets.CertificatePart, Secret: install.TLSCertificateRef, Variable: "artifactCertificate"},
	}
	if port.CAVariable != "" {
		materials = append(materials, lifecycle.MaterialFile{Name: "bmc-ca", Part: secrets.CertificatePart, Secret: install.Target.Controller.TrustBundleRef, Variable: port.CAVariable})
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
		return eraseLineage(data, effective, execution, values["pinnedUUIDBase64"])
	}
	result, err := c.runner.Run(ctx, lifecycle.RunFor(execution, lifecycle.Invocation{Implementation: EraseImplementation, Operation: operation, Variable: eraseVariable, Canonical: canonical,
		Placement: install.Placement, Materials: materials, Values: values, Deadline: deadline, RemovalPreparation: validate,
		Groups: eraseGroupIDs(execution.Block), GroupNote: eraseGroupNote(install.Address),
		Refusals: eraseRefusals(execution.Context, execution.Continuation, install.Identity.Object, install.Target.Controller.Endpoint, install.Address,
			eraseCustody(execution.Context, install.Identity.Object, execution.ApplyOperation), erasePublication(request))}))
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
		next := execution.Continuation
		if next == "" {
			next = "bootwright destroy --context " + execution.Context
		}
		return refusal("lifecycle.state", "physical erase receipt has another removal attempt lineage",
			"run "+next+" again; a removal erases under its own attempt lineage and sets an earlier removal's receipt aside without adopting it")
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

func (c Capability) WithDiscoveredRoots(roots installation.DiscoveredRoots) Capability {
	c.discoveredRoots = roots
	return c
}

func (Capability) Claimed(catalog api.Catalog) []string {
	var claimed []string
	for _, machine := range installation.InstalledMachines(catalog) {
		if machine.Spec().Has("hardware", "eraseOnRemoval") {
			claimed = append(claimed, machine.Identity())
		}
	}
	return claimed
}

func (Capability) SecretReferences(block reconciliation.Block) ([]string, error) {
	request, err := DecodeEraseRequest(block.Request)
	if err != nil {
		return nil, err
	}
	return request.SecretReferences(), nil
}

func (Capability) SecretClasses(block reconciliation.Block) (secrets.Classes, error) {
	request, err := DecodeEraseRequest(block.Request)
	if err != nil {
		return nil, err
	}
	return request.Install.SecretClasses(), nil
}
