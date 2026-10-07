package prerequisites

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
)

func object(values map[string]any) json.RawMessage {
	data, _ := json.Marshal(values)
	return data
}

func newReceipt(i inspection) (SetupReceipt, error) {
	actions := []SetupAction{{ID: "execution-bundle", Request: object(map[string]any{"catalogDigest": i.definition.CatalogDigest, "readyBefore": i.bundle.Ready}), Phase: "planned", Evidence: object(map[string]any{})}}
	if i.selection.ContainerRuntime() {
		actions = append(actions, SetupAction{ID: "container-runtime", Request: object(map[string]any{"readyBefore": i.dependenciesReady(), "version": i.definition.Runtime.Version}), Phase: "planned", Evidence: object(map[string]any{})})
	}
	receipt := SetupReceipt{CatalogDigest: i.definition.CatalogDigest, Context: i.view.Context, Egress: i.route(), Sources: slices.Clone(i.definition.Sources), Actions: actions, Status: "pending"}
	if i.definition.Bootstrap != nil {
		definition := CloneDefinition(i.definition)
		receipt.Definition = &definition
		receipt.Foundation = CloneQualifiedFoundation(i.qualified)
	}
	digest, err := SetupPlanDigest(i.host, receipt)
	if err != nil {
		return SetupReceipt{}, err
	}
	receipt.PlanDigest = digest
	identity := sha256.Sum256([]byte("bootwright.controller.receipt-v1\x00" + digest + "\x00" + i.view.State.Receipt.ID))
	receipt.ID = "setup-" + hex.EncodeToString(identity[:16])
	return receipt, nil
}

// matchesActions compares a pending receipt's actions with the ones setup
// plans, whichever context inspects it, because setup selects none.
func (i inspection) matchesActions(actions []SetupAction) bool {
	setup := i
	setup.view.Context = SetupContext{}
	expected, err := newReceipt(setup)
	if err != nil || len(actions) != len(expected.Actions) {
		return false
	}
	for index, action := range actions {
		if action.ID != expected.Actions[index].ID {
			return false
		}
		var actual, required map[string]any
		if json.Unmarshal(action.Request, &actual) != nil || json.Unmarshal(expected.Actions[index].Request, &required) != nil {
			return false
		}
		value, valid := actual["readyBefore"].(bool)
		if !valid {
			return false
		}
		required["readyBefore"] = value
		if !bytes.Equal(action.Request, object(required)) {
			return false
		}
	}
	return true
}

// retainedArea opens the bundle a carried resolution was reprojected
// from, so preparation can read its approved sources instead of acquiring them
// again. It is an optimization and never a prerequisite: an area that is absent
// or cannot be opened simply leaves every source to its publisher.
func retainedArea(ctx context.Context, tx StorageTransaction, current inspection) BundleArea {
	if current.retainedDigest == "" || current.retainedDigest == current.definition.CatalogDigest {
		return nil
	}
	open := tx.Snapshot().OpenBundle
	if open == nil {
		return nil
	}
	area, err := open(ctx, current.retainedDigest)
	if err != nil {
		return nil
	}
	return area
}

func (s Service) prepare(ctx context.Context, tx StorageTransaction, current *inspection) error {
	ctx = WithLaunchFoundation(ctx, current.qualified)
	state := current.view.State
	// The receipt is the authority on what actually happened, so progress is
	// taken from it however this attempt ends.
	defer func() {
		current.report.Progress = nil
		for _, action := range state.Receipt.Actions {
			current.report.Progress = append(current.report.Progress, ActionProgress{ID: action.ID, Phase: action.Phase, Outcome: action.Outcome})
		}
	}()
	if state.Receipt.ID == "" || !state.Receipt.Incomplete() {
		receipt, err := newReceipt(*current)
		if err != nil {
			return err
		}
		state.Host, state.Receipt = current.host, receipt
		if state.Bindings == nil {
			state.Bindings = []ControllerBinding{}
		}
		if state.RetainedSources == nil {
			state.RetainedSources = []DependencySource{}
		}
		if err := publish(ctx, tx, state); err != nil {
			return err
		}
		state = tx.Snapshot().State
	}
	for index := range state.Receipt.Actions {
		if err := ctx.Err(); err != nil {
			return err
		}
		action := &state.Receipt.Actions[index]
		progress := func(event ProgressEvent) {
			event.Phase, event.Action, event.Step, event.Steps = SetupPhase, action.ID, index+1, len(state.Receipt.Actions)
			s.report(ctx, &current.report, event)
		}
		ready := action.ID == "execution-bundle" && current.bundle.Ready || action.ID == "container-runtime" && current.dependenciesReady()
		var request map[string]any
		if json.Unmarshal(action.Request, &request) != nil {
			return failure("controller.unknown", "retained setup request is invalid", "restore the exact setup evidence")
		}
		if !ready && request["readyBefore"] == true {
			return failure("controller.unknown", "a prerequisite recorded as ready before setup is missing", "restore the original prerequisite before resolving the pending setup")
		}
		if action.Phase == "observed" && (action.Outcome == "changed" || action.Outcome == "unchanged") {
			if !ready {
				return failure("controller.unknown", "a recorded setup postcondition no longer holds", "restore the exact dependency before repeating setup")
			}
			continue
		}
		outcome := "unchanged"
		evidence := object(map[string]any{"postcondition": "verified"})
		if action.ID == "container-runtime" && len(action.Preparation) != 0 {
			recovered, err := s.recoverNative(ctx, tx, current, action.Preparation, progress)
			if err != nil {
				return err
			}
			ready = true
			evidence = recovered
			outcome = "changed"
		}
		if !ready {
			var err error
			if evidence, err = s.performAction(ctx, tx, current, &state, action, progress, evidence); err != nil {
				return err
			}
			outcome = "changed"
		}
		progress(ProgressEvent{Status: outcome})
		action.Phase, action.Outcome = "observed", outcome
		action.Evidence = evidence
		if err := publish(ctx, tx, state); err != nil {
			return err
		}
	}
	state.Receipt.Status = "complete"
	return publish(ctx, tx, state)
}

func (s Service) recoverNative(ctx context.Context, tx StorageTransaction, current *inspection, recorded json.RawMessage, progress func(ProgressEvent)) (json.RawMessage, error) {
	if s.runtime == nil {
		return nil, failure("controller.unknown", "native recovery verification is not configured", "restore the exact compatible executable")
	}
	if (!current.runtime.Ready && current.definition.Native == nil) || !current.bundle.Ready || !current.bundle.Recoverable {
		return nil, failure("controller.unknown", "native effects or retained tool files cannot be attributed", "restore the exact native prerequisites and setup evidence")
	}
	area, err := tx.Bundle(ctx, current.definition.CatalogDigest)
	if err != nil {
		return nil, err
	}
	if area == nil {
		return nil, failure("controller.unknown", "native recovery execution bundle is missing", "restore the exact retained bundle")
	}
	preparation, err := ReadNativePreparation(recorded, current.definition)
	if err != nil {
		return nil, err
	}
	progress(ProgressEvent{Status: "running", Detail: "recovering the recorded native transaction"})
	output, release := s.retain(ctx, tx, &current.report)
	result, err := s.runtime.Recover(ctx, area, current.platform, current.definition, current.route(), preparation, progress, output)
	release()
	if err != nil {
		return nil, err
	}
	if result.Outcome != "unchanged" && result.Outcome != "changed" || len(result.Evidence) <= 2 {
		return nil, failure("controller.unknown", "native recovery postcondition is unproved", "resolve the original native transaction before repeating setup")
	}
	current.bundle, err = s.bundle.Inspect(ctx, area, current.definition, true)
	if err != nil {
		return nil, err
	}
	current.runtime, err = s.inspectRuntime(ctx, current.definition)
	if err != nil {
		return nil, err
	}
	if !current.bundle.Ready || !current.dependenciesReady() {
		return nil, failure("controller.unknown", "recovered dependency postconditions are incomplete", "restore the exact setup dependencies before retrying")
	}
	return result.Evidence, nil
}

func (s Service) performAction(ctx context.Context, tx StorageTransaction, current *inspection, state *HostState, action *SetupAction, progress func(ProgressEvent), evidence json.RawMessage) (json.RawMessage, error) {
	if action.Phase != "planned" && action.ID == "execution-bundle" && !current.bundle.Recoverable {
		return nil, failure("controller.unknown", "interrupted bundle publication cannot be attributed safely", "preserve the setup state and restore its exact dependency evidence")
	}
	if action.Phase == "planned" {
		action.Phase = "intent"
		if err := publish(ctx, tx, *state); err != nil {
			return nil, err
		}
	}
	progress(ProgressEvent{Status: "running"})
	var err error
	switch action.ID {
	case "execution-bundle":
		var area BundleArea
		area, err = tx.Bundle(ctx, current.definition.CatalogDigest)
		if err == nil {
			// Preparation reads back and qualifies exactly what it
			// published, so its verification is the postcondition
			// rather than a second full pass over the same closure.
			current.bundle, err = s.bundle.Prepare(ctx, area, retainedArea(ctx, tx, *current), current.definition, current.route(), progress)
			if err == nil && !current.bundle.Ready {
				err = failure("controller.unknown", "execution bundle postcondition could not be verified", "repeat the exact setup after inspecting the retained evidence")
			}
		}
	case "container-runtime":
		if s.runtime == nil {
			err = failure("controller.unsupported", "native runtime installation is not configured", "prepare the qualified runtime before repeating setup")
		} else {
			var area BundleArea
			area, err = tx.Bundle(ctx, current.definition.CatalogDigest)
			if err == nil && area == nil {
				err = failure("controller.unknown", "verified runtime execution bundle is missing", "restore the exact setup bundle")
			}
			if err == nil {
				var result ActionResult
				output, release := s.retain(ctx, tx, &current.report)
				result, err = s.runtime.Prepare(ctx, area, current.platform, current.definition, current.route(), func(call context.Context, preparation NativePreparation) error {
					encoded := EncodeNativePreparation(preparation)
					if _, err := ReadNativePreparation(encoded, current.definition); err != nil {
						return err
					}
					if len(action.Preparation) != 0 && !bytes.Equal(action.Preparation, encoded) {
						return failure("controller.unknown", "native before-state differs from its retained preparation", "resolve the original native transaction")
					}
					action.Preparation = encoded
					return publish(call, tx, *state)
				}, progress, output)
				release()
				if err == nil && len(result.Evidence) > 2 {
					evidence = result.Evidence
				}
				if err != nil && result.Outcome == "failed" && len(result.Evidence) > 2 {
					action.Phase, action.Outcome, action.Evidence = "observed", "failed", result.Evidence
					state.Receipt.Status = "failed"
					if publicationErr := publish(ctx, tx, *state); publicationErr != nil {
						return nil, publicationErr
					}
				} else if err == nil && result.Outcome != "changed" && result.Outcome != "unchanged" {
					err = failure("controller.unknown", "native runtime action has no definitive result", "resolve the exact native transaction before repeating setup")
				}
				if err == nil {
					current.bundle, err = s.bundle.Inspect(ctx, area, current.definition, true)
				}
			}
		}
		if err == nil {
			current.runtime, err = s.inspectRuntime(ctx, current.definition)
			if err == nil && !current.dependenciesReady() {
				err = failure("controller.unknown", "container runtime postcondition could not be verified", "resolve the native transaction before repeating setup")
			}
		}
	default:
		err = failure("controller.unknown", "setup contains an unsupported retained action", "restore the original compatible executable")
	}
	if err != nil {
		progress(ProgressEvent{Status: "failed"})
		return nil, err
	}
	return evidence, nil
}

// report never fails an operation: progress is presentation, and a setup that
// could not describe itself has still done exactly what it recorded. The
// report remembers that rows were streamed so the result does not repeat them.
func (s Service) report(ctx context.Context, report *Report, event ProgressEvent) {
	if s.options.Progress == nil || ctx.Err() != nil {
		return
	}
	report.ProgressPresented = true
	s.options.Progress.ReportProgress(ctx, event)
}

// retain opens the run the controller Ansible about to start writes to, and
// names it first so it can be followed while it runs. The receipt never
// records a run, so a run that cannot be opened, written or closed leaves that
// output discarded and changes nothing else.
func (s Service) retain(ctx context.Context, tx StorageTransaction, report *Report) (RunOutput, func()) {
	run, err := tx.OpenRun(ctx)
	if err != nil || run == nil {
		return nil, func() {}
	}
	report.LogLocation = run.Location()
	if s.options.Progress != nil && ctx.Err() == nil && report.LogLocation != "" {
		s.options.Progress.ReportLogLocation(ctx, report.LogLocation)
	}
	return run, func() { _ = run.Close() }
}

func publish(ctx context.Context, tx StorageTransaction, state HostState) error {
	publication, err := tx.Publish(ctx, state)
	if publication == Unknown {
		return failure("controller.unknown", "setup evidence publication is uncertain", "preserve state and repeat the exact setup only after storage is healthy")
	}
	if err != nil {
		return err
	}
	if publication != Committed {
		return failure("controller.setup", "setup evidence was not committed", "repeat the exact setup after resolving the storage failure")
	}
	return nil
}

// ReadNativePreparation admits a container-runtime action's before-state only
// when its bytes are exactly what EncodeNativePreparation writes, its added
// sources belong to definition, and its native fields match definition's
// native plan or, without one, are absent. Setup reads each preparation back
// this way before retaining it and again before recovering from it.
func ReadNativePreparation(data []byte, definition Definition) (NativePreparation, error) {
	var preparation NativePreparation
	invalid := func() (NativePreparation, error) {
		return NativePreparation{}, failure("controller.unknown", "native preparation evidence is incomplete or incompatible", "restore the exact native setup evidence before continuing")
	}
	if len(data) == 0 || len(data) > 64<<10 || json.Unmarshal(data, &preparation) != nil {
		return invalid()
	}
	digest, err := hex.DecodeString(preparation.InventorySHA256)
	if err != nil || len(digest) != 32 || hex.EncodeToString(digest) != preparation.InventorySHA256 || preparation.AddedSources == nil || len(preparation.AddedSources) > 512 {
		return invalid()
	}
	previous := ""
	for _, id := range preparation.AddedSources {
		if id <= previous || !slices.ContainsFunc(definition.Sources, func(source DependencySource) bool { return source.ID == id }) {
			return invalid()
		}
		previous = id
	}
	if definition.Native != nil {
		transitions, err := NativeTransitionsDigest(definition.Native.Actions)
		if err != nil || preparation.PlanDigest != definition.Native.Digest || preparation.InventorySHA256 != definition.Native.BeforeSHA256 || preparation.AfterInventorySHA256 != definition.Native.AfterSHA256 || preparation.TransitionsSHA256 != transitions {
			return invalid()
		}
	} else if preparation.PlanDigest != "" || preparation.AfterInventorySHA256 != "" || preparation.TransitionsSHA256 != "" {
		return invalid()
	}
	encoded := EncodeNativePreparation(preparation)
	if !bytes.Equal(data, encoded) {
		return invalid()
	}
	return preparation, nil
}

// EncodeNativePreparation is the canonical object the container-runtime
// action retains as its preparation, published once while its intent is held.
func EncodeNativePreparation(preparation NativePreparation) json.RawMessage {
	value := map[string]any{"inventorySHA256": preparation.InventorySHA256, "addedSources": slices.Clone(preparation.AddedSources)}
	if preparation.PlanDigest != "" || preparation.AfterInventorySHA256 != "" || preparation.TransitionsSHA256 != "" {
		value["planDigest"] = preparation.PlanDigest
		value["afterInventorySHA256"] = preparation.AfterInventorySHA256
		value["transitionsSHA256"] = preparation.TransitionsSHA256
	}
	return object(value)
}
