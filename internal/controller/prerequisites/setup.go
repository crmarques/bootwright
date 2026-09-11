package prerequisites

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"strings"
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
	if i.view.Context.Name != "" {
		actions = append(actions, SetupAction{ID: "controller-binding", Request: object(map[string]any{"boundBefore": i.bound, "machine": i.selection.MachineName()}), Phase: "planned", Evidence: object(map[string]any{})})
	}
	receipt := SetupReceipt{CatalogDigest: i.definition.CatalogDigest, Context: i.view.Context, Egress: i.route(), Sources: slices.Clone(i.definition.Sources), Actions: actions, Status: "pending"}
	if i.definition.Bootstrap != nil {
		definition := CloneDefinition(i.definition)
		receipt.Definition = &definition
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

func (i inspection) matchesActions(actions []SetupAction) bool {
	expected, err := newReceipt(i)
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
		before := "readyBefore"
		if action.ID == "controller-binding" {
			before = "boundBefore"
		}
		value, valid := actual[before].(bool)
		if !valid {
			return false
		}
		required[before] = value
		if !bytes.Equal(action.Request, object(required)) {
			return false
		}
	}
	return true
}

func (s Service) prepare(ctx context.Context, tx StorageTransaction, current *inspection) error {
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
		ready := action.ID == "execution-bundle" && current.bundle.Ready || action.ID == "container-runtime" && current.dependenciesReady() || action.ID == "controller-binding" && current.bound
		var request map[string]any
		if json.Unmarshal(action.Request, &request) != nil {
			return failure("controller.unknown", "retained setup request is invalid", "restore the exact setup evidence")
		}
		if !ready && (request["readyBefore"] == true || request["boundBefore"] == true) {
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
			if s.runtime == nil {
				return failure("controller.unknown", "native recovery verification is not configured", "restore the exact compatible executable")
			}
			if (!current.runtime.Ready && current.definition.Native == nil) || !current.bundle.Ready || !current.bundle.Recoverable {
				return failure("controller.unknown", "native effects or retained tool files cannot be attributed", "restore the exact native prerequisites and setup evidence")
			}
			area, err := tx.Bundle(ctx, current.definition.CatalogDigest)
			if err != nil {
				return err
			}
			if area == nil {
				return failure("controller.unknown", "native recovery execution bundle is missing", "restore the exact retained bundle")
			}
			preparation, err := nativePreparation(action.Preparation, current.definition)
			if err != nil {
				return err
			}
			result, err := s.runtime.Recover(ctx, area, current.platform, current.definition, current.route(), preparation)
			if err != nil {
				return err
			}
			if result.Outcome != "unchanged" && result.Outcome != "changed" || len(result.Evidence) <= 2 {
				return failure("controller.unknown", "native recovery postcondition is unproved", "resolve the original native transaction before repeating setup")
			}
			current.bundle, err = s.bundle.Inspect(ctx, area, current.definition, true)
			if err != nil {
				return err
			}
			current.runtime, err = s.inspectRuntime(ctx, current.definition)
			if err != nil {
				return err
			}
			if !current.bundle.Ready || !current.dependenciesReady() {
				return failure("controller.unknown", "recovered dependency postconditions are incomplete", "restore the exact setup dependencies before retrying")
			}
			ready = true
			evidence = result.Evidence
			outcome = "changed"
		}
		if !ready {
			if action.Phase != "planned" && action.ID == "execution-bundle" && !current.bundle.Recoverable {
				return failure("controller.unknown", "interrupted bundle publication cannot be attributed safely", "preserve the setup state and restore its exact dependency evidence")
			}
			if action.Phase == "planned" {
				action.Phase = "intent"
				if err := publish(ctx, tx, state); err != nil {
					return err
				}
			}
			var err error
			switch action.ID {
			case "execution-bundle":
				var area BundleArea
				area, err = tx.Bundle(ctx, current.definition.CatalogDigest)
				if err == nil {
					err = s.bundle.Prepare(ctx, area, current.definition, current.route())
				}
				if err == nil {
					current.bundle, err = s.bundle.Inspect(ctx, area, current.definition, true)
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
						result, err = s.runtime.Prepare(ctx, area, current.platform, current.definition, current.route(), func(call context.Context, preparation NativePreparation) error {
							encoded := preparationObject(preparation)
							if _, err := nativePreparation(encoded, current.definition); err != nil {
								return err
							}
							if len(action.Preparation) != 0 && !bytes.Equal(action.Preparation, encoded) {
								return failure("controller.unknown", "native before-state differs from its retained preparation", "resolve the original native transaction")
							}
							action.Preparation = encoded
							return publish(call, tx, state)
						})
						if err == nil && len(result.Evidence) > 2 {
							evidence = result.Evidence
						}
						if err != nil && result.Outcome == "failed" && len(result.Evidence) > 2 {
							action.Phase, action.Outcome, action.Evidence = "observed", "failed", result.Evidence
							state.Receipt.Status = "failed"
							if publicationErr := publish(ctx, tx, state); publicationErr != nil {
								return publicationErr
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
			case "controller-binding":
				current.bound = true
			default:
				err = failure("controller.unknown", "setup contains an unsupported retained action", "restore the original compatible executable")
			}
			if err != nil {
				return err
			}
			outcome = "changed"
		}
		action.Phase, action.Outcome = "observed", outcome
		action.Evidence = evidence
		if action.ID != "controller-binding" {
			if err := publish(ctx, tx, state); err != nil {
				return err
			}
		}
	}
	if current.view.Context.Name != "" && !slices.ContainsFunc(state.Bindings, func(binding ControllerBinding) bool { return binding.ContextID == current.view.Context.ID }) {
		digest, _ := current.host.PrivateDigest()
		state.Bindings = append(state.Bindings, ControllerBinding{ContextID: current.view.Context.ID, Machine: current.selection.MachineName(), HostDigest: digest})
		slices.SortFunc(state.Bindings, func(a, b ControllerBinding) int { return strings.Compare(a.ContextID, b.ContextID) })
	}
	state.Receipt.Status = "complete"
	return publish(ctx, tx, state)
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

func nativePreparation(data []byte, definition Definition) (NativePreparation, error) {
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
	encoded := preparationObject(preparation)
	if !bytes.Equal(data, encoded) {
		return invalid()
	}
	return preparation, nil
}

func preparationObject(preparation NativePreparation) json.RawMessage {
	value := map[string]any{"inventorySHA256": preparation.InventorySHA256, "addedSources": slices.Clone(preparation.AddedSources)}
	if preparation.PlanDigest != "" || preparation.AfterInventorySHA256 != "" || preparation.TransitionsSHA256 != "" {
		value["planDigest"] = preparation.PlanDigest
		value["afterInventorySHA256"] = preparation.AfterInventorySHA256
		value["transitionsSHA256"] = preparation.TransitionsSHA256
	}
	return object(value)
}
