package cli

import "slices"

func renderValidate() commandRenderer {
	return commandRenderer{human: func(r *Runner, c renderCall) (bool, error) {
		if c.result.validation == nil {
			return false, nil
		}
		return true, writeValidation(r.config.Out, r.config.ErrOut, c.path, c.result.validation, selectedJSON(c.command))
	}}
}

func renderController() commandRenderer {
	return commandRenderer{
		structured: func(r *Runner, c renderCall) (bool, error) {
			report := c.result.controller
			if report == nil || report.DryRun || !validControllerReport(report) {
				return false, nil
			}
			return true, writeStructuredController(r.config.Out, c.path, report, nil, 0)
		},
		human: func(r *Runner, c renderCall) (bool, error) {
			report := c.result.controller
			if !validControllerReport(report) || !successfulControllerReport(c.path, report) {
				return false, nil
			}
			return true, writeControllerResult(r.config.Out, r.config.ErrOut, c.path, report, selectedJSON(c.command))
		},
	}
}

func renderRebind() commandRenderer {
	return commandRenderer{
		structured: structuredShown(func(result commandResult) (jsonResult, []diagnostic, bool) {
			if !validRebind(result.rebind) {
				return nil, nil, false
			}
			return rebindPresentation{Outcome: escapeDisplayLine(result.rebind.Outcome), Bindings: result.rebind.Bindings, Reservations: result.rebind.Reservations}, nil, true
		}),
		human: func(r *Runner, c renderCall) (bool, error) {
			if !validRebind(c.result.rebind) {
				return false, nil
			}
			return true, writeRebind(r.config.Out, c.result.rebind)
		},
	}
}

func renderUninstall() commandRenderer {
	return commandRenderer{
		structured: structuredShown(func(result commandResult) (jsonResult, []diagnostic, bool) {
			if !validUninstall(result.uninstall) {
				return nil, nil, false
			}
			return displayUninstall(result.uninstall), nil, true
		}),
		human: func(r *Runner, c renderCall) (bool, error) {
			if !validUninstall(c.result.uninstall) {
				return false, nil
			}
			return true, writeUninstall(r.config.Out, c.result.uninstall)
		},
	}
}

func renderPlan() commandRenderer {
	return commandRenderer{
		structured: structuredShown(func(result commandResult) (jsonResult, []diagnostic, bool) {
			if result.lifecyclePlan == nil {
				return nil, nil, false
			}
			return displayPlan(result.lifecyclePlan), result.lifecyclePlan.Warnings, true
		}),
		human: func(r *Runner, c renderCall) (bool, error) {
			plan := c.result.lifecyclePlan
			if plan == nil {
				return false, nil
			}
			if err := writeHumanDiagnostics(r.config.ErrOut, displayDiagnostics(plan.Warnings)); err != nil {
				return true, err
			}
			return true, writeLifecyclePlan(r.config.Out, plan)
		},
	}
}

func renderStatus() commandRenderer {
	return commandRenderer{human: func(r *Runner, c renderCall) (bool, error) {
		status := c.result.lifecycleStatus
		if status == nil {
			return false, nil
		}
		if !selectedJSON(c.command) {
			if err := writeHumanDiagnostics(r.config.ErrOut, displayDiagnostics(status.Warnings)); err != nil {
				return true, err
			}
		}
		return true, writeLifecycleStatus(r.config.Out, status, selectedJSON(c.command))
	}}
}

func renderDiff() commandRenderer {
	return commandRenderer{human: func(r *Runner, c renderCall) (bool, error) {
		diff := c.result.lifecycleDiff
		if diff == nil {
			return false, nil
		}
		if !selectedJSON(c.command) {
			if err := writeHumanDiagnostics(r.config.ErrOut, displayDiagnostics(diff.Warnings)); err != nil {
				return true, err
			}
		}
		flags := c.command.Flags()
		status, err := writeDiffStatus(c.ctx, r.config.Out, diff, diffOptions{
			json: selectedJSON(c.command), exitCode: boolValue(flags, "exit-code"),
			outputDir: stringValue(flags, "output-dir"), tool: stringValue(flags, "tool"),
			yamlEncode: r.config.EncodeEffectiveYAML, jsonEncode: r.config.EncodeEffectiveJSON,
			comparison: r.config.Services.Comparison, style: r.config.Presentation,
		})
		r.resultStatus = status
		return true, err
	}}
}

func renderLogs() commandRenderer {
	return commandRenderer{human: func(r *Runner, c renderCall) (bool, error) {
		switch {
		case c.result.logsFollow != nil:
			return true, writeLogsFollowed(r.config.ErrOut, c.result.logsFollow)
		case c.result.logs != nil:
			return true, writeLogs(r.config.Out, r.config.ErrOut, c.result.logs, selectedStructured(c.command))
		}
		return false, nil
	}}
}

func lifecycleFailed(r *Runner, c renderCall, reported []diagnostic) (bool, error) {
	return r.writeExecutedLifecycle(c.path, c.result, reported)
}

func renderLifecycle() commandRenderer {
	plan, operation := renderPlan(), renderOperation()
	return commandRenderer{
		structured: func(r *Runner, c renderCall) (bool, error) {
			if boolValue(c.command.Flags(), "dry-run") {
				return plan.structured(r, c)
			}
			return operation.structured(r, c)
		},
		human: func(r *Runner, c renderCall) (bool, error) {
			if boolValue(c.command.Flags(), "dry-run") {
				return plan.human(r, c)
			}
			return operation.human(r, c)
		},
		failed: func(r *Runner, c renderCall, reported []diagnostic) (bool, error) {
			if c.result.lifecyclePlan == nil {
				return operation.failed(r, c, reported)
			}
			if selectedStructured(c.command) {
				plan := c.result.lifecyclePlan
				return true, writeStructuredResult(r.config.Out, c.path, displayPlan(plan), append(slices.Clone(plan.Warnings), reported...), nil, 1)
			}
			if _, err := plan.human(r, c); err != nil {
				return true, err
			}
			return true, writeHumanDiagnostics(r.config.ErrOut, reported)
		},
	}
}

func renderOperation() commandRenderer {
	return commandRenderer{
		structured: func(r *Runner, c renderCall) (bool, error) {
			if c.result.lifecycleOperation == nil {
				return false, nil
			}
			return true, writeStructuredOperation(r.config.Out, c.path, c.result.lifecycleOperation, nil, 0)
		},
		human: func(r *Runner, c renderCall) (bool, error) {
			operation := c.result.lifecycleOperation
			if operation == nil {
				return false, nil
			}
			if err := writeHumanDiagnostics(r.config.ErrOut, displayDiagnostics(operation.Warnings)); err != nil {
				return true, err
			}
			return true, writeLifecycleOperation(r.config.Out, operation)
		},
		failed: lifecycleFailed,
	}
}

func renderMachineDiscovery() commandRenderer {
	return commandRenderer{
		structured: func(r *Runner, c renderCall) (bool, error) {
			switch result := c.result; {
			case result.machineDiscovery != nil:
				return true, writeMachineDiscovery(r.config.Out, c.path, result.machineDiscovery, true)
			case result.lifecyclePlan != nil:
				return true, writeStructuredResult(r.config.Out, c.path, displayPlan(result.lifecyclePlan), result.lifecyclePlan.Warnings, nil, 0)
			case result.lifecycleOperation != nil:
				return true, writeStructuredOperation(r.config.Out, c.path, result.lifecycleOperation, nil, 0)
			}
			return false, nil
		},
		human: func(r *Runner, c renderCall) (bool, error) {
			switch result := c.result; {
			case result.machineDiscovery != nil:
				return true, writeMachineDiscovery(r.config.Out, c.path, result.machineDiscovery, selectedStructured(c.command))
			case result.lifecyclePlan != nil:
				return true, writeLifecyclePlan(r.config.Out, result.lifecyclePlan)
			case result.lifecycleOperation != nil:
				return true, writeLifecycleOperation(r.config.Out, result.lifecycleOperation)
			}
			return false, nil
		},
		failed: lifecycleFailed,
	}
}

func renderMachineList() commandRenderer {
	return commandRenderer{human: func(r *Runner, c renderCall) (bool, error) {
		if !validMachineList(c.result.machines) {
			return false, nil
		}
		return true, writeMachineList(r.config.Out, c.path, c.result.machines, boolValue(c.command.Flags(), "short"), selectedJSON(c.command))
	}}
}

func renderMachineInfo() commandRenderer {
	return commandRenderer{human: func(r *Runner, c renderCall) (bool, error) {
		if !validMachineList(c.result.machines) {
			return false, nil
		}
		return true, writeMachineInfo(r.config.Out, c.path, c.result.machines, selectedStructured(c.command))
	}}
}

func renderMachineTrust() commandRenderer {
	return commandRenderer{human: func(r *Runner, c renderCall) (bool, error) {
		if !validTrustReport(c.result.trust) {
			return false, nil
		}
		return true, writeTrustReport(r.config.Out, c.path, c.result.trust, selectedJSON(c.command))
	}}
}

func renderMachinePower() commandRenderer {
	return commandRenderer{
		human: func(r *Runner, c renderCall) (bool, error) {
			if !validMachinePower(c.result.power, false) {
				return false, nil
			}
			return true, writeMachinePower(r.config.Out, c.path, c.result.power, selectedJSON(c.command))
		},
		failed: func(r *Runner, c renderCall, reported []diagnostic) (bool, error) {
			return r.writeFailedPowerBatch(c.command, c.path, c.result, reported)
		},
	}
}

func renderMachineCopy() commandRenderer {
	return commandRenderer{outcome: func(r *Runner, c renderCall, err error) (bool, int) {
		return r.writeCopyOutcome(c.ctx, c.command, c.path, c.result, err)
	}}
}

func renderClusterHandoff() commandRenderer {
	return commandRenderer{
		structured: structuredShown(func(result commandResult) (jsonResult, []diagnostic, bool) {
			if result.handoff == nil {
				return nil, nil, false
			}
			return displayHandoff(result.handoff), nil, true
		}),
		human: func(r *Runner, c renderCall) (bool, error) {
			if c.result.handoff == nil {
				return false, nil
			}
			return true, writeHandoff(r.config.Out, c.result.handoff)
		},
	}
}

func renderClusterList() commandRenderer {
	return commandRenderer{human: func(r *Runner, c renderCall) (bool, error) {
		if !validClusterList(c.result.clusterList) {
			return false, nil
		}
		return true, writeClusterList(r.config.Out, c.path, c.result.clusterList, selectedJSON(c.command))
	}}
}

func renderClusterInfo() commandRenderer {
	return commandRenderer{human: func(r *Runner, c renderCall) (bool, error) {
		return writeClusterInfoResult(r.config.Out, r.config.ErrOut, c.path, c.result, selectedJSON(c.command))
	}}
}

func renderAdminCredentials() commandRenderer {
	return commandRenderer{human: func(r *Runner, c renderCall) (bool, error) {
		if c.result.adminCredentials == nil {
			return false, nil
		}
		return true, writeAdminCredentials(r.config.Out, c.path, c.result.adminCredentials, selectedJSON(c.command))
	}}
}

func renderKubeconfig() commandRenderer {
	return commandRenderer{
		structured: structuredReveal(func(result commandResult) bool { return result.kubeconfig != nil }, func(c renderCall) (jsonResult, []diagnostic, bool) {
			value, ok := kubeconfigValue(c.result.kubeconfig)
			if !ok {
				return nil, nil, false
			}
			defer clear(value)
			return exportedResult("application/yaml", value), kubeconfigWarnings(c.result.kubeconfig), true
		}),
		human: func(r *Runner, c renderCall) (bool, error) {
			if c.result.kubeconfig == nil {
				return false, nil
			}
			return true, writeKubeconfig(r.config.Out, r.config.ErrOut, c.result.kubeconfig)
		},
	}
}

func renderKeyring() commandRenderer {
	return commandRenderer{
		structured: structuredReveal(func(result commandResult) bool { return result.keyring != nil }, func(c renderCall) (jsonResult, []diagnostic, bool) {
			value, ok := keyringValue(c.result.keyring)
			if !ok {
				return nil, nil, false
			}
			defer clear(value)
			return exportedResult("text/plain", value), keyringWarnings(c.result.keyring), true
		}),
		human: func(r *Runner, c renderCall) (bool, error) {
			if c.result.keyring == nil {
				return false, nil
			}
			return true, writeKeyring(r.config.Out, r.config.ErrOut, c.result.keyring)
		},
	}
}

func renderEffective() commandRenderer {
	return commandRenderer{
		structured: structuredShown(func(result commandResult) (jsonResult, []diagnostic, bool) {
			if !validRender(result.rendered) {
				return nil, nil, false
			}
			return displayRender(result.rendered), nil, true
		}),
		human: func(r *Runner, c renderCall) (bool, error) {
			result := c.result
			switch {
			case validRender(result.rendered):
				return true, writeRender(r.config.Out, result.rendered)
			case result.effective != nil:
				encoder := r.config.EncodeEffectiveYAML
				if selectedJSON(c.command) {
					encoder = r.config.EncodeEffectiveJSON
				}
				return true, writeEffective(c.ctx, r.config.Out, c.path, result.effective, selectedJSON(c.command), encoder)
			}
			return false, nil
		},
	}
}

func renderAcquisitions() commandRenderer {
	return commandRenderer{human: func(r *Runner, c renderCall) (bool, error) {
		if c.result.effective == nil {
			return false, nil
		}
		return true, writeAcquisitions(r.config.Out, c.path, r.config.Acquisitions, c.result.effective.Effective, selectedStructured(c.command))
	}}
}
