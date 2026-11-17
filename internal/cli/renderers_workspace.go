package cli

import (
	"slices"
	"strings"

	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

type secretVerb int

const (
	secretSet secretVerb = iota
	secretGenerate
	secretDelete
)

func renderAdmission(update bool) commandRenderer {
	return commandRenderer{
		structured: structuredShown(func(result commandResult) (jsonResult, []diagnostic, bool) {
			if !validAdmission(result.admission) {
				return nil, nil, false
			}
			a := result.admission
			return contextAdmissionPresentation{Context: contextSummary(a.Context), Counts: displayCounts(a.Counts), FilesCopied: a.FilesCopied, InputChanged: a.InputChanged}, a.Diagnostics, true
		}),
		human: func(r *Runner, c renderCall) (bool, error) {
			if !validAdmission(c.result.admission) {
				return false, nil
			}
			return true, writeAdmission(r.config.Out, r.config.ErrOut, update, c.result.admission)
		},
	}
}

func renderContextUse() commandRenderer {
	return commandRenderer{
		structured: structuredShown(func(result commandResult) (jsonResult, []diagnostic, bool) {
			if result.use == nil || !validContextSummary(result.use.Context) {
				return nil, nil, false
			}
			return contextPresentation{Context: contextSummary(result.use.Context)}, nil, true
		}),
		human: func(r *Runner, c renderCall) (bool, error) {
			if c.result.use == nil || !validContextSummary(c.result.use.Context) {
				return false, nil
			}
			return true, writeContextUse(r.config.Out, c.result.use)
		},
	}
}

func renderContextCurrent() commandRenderer {
	return commandRenderer{
		structured: structuredShown(func(result commandResult) (jsonResult, []diagnostic, bool) {
			if result.current == nil || !validContextSummary(result.current.Context) {
				return nil, nil, false
			}
			return contextPresentation{Context: contextSummary(result.current.Context)}, nil, true
		}),
		human: func(r *Runner, c renderCall) (bool, error) {
			if c.result.current == nil || !validContextSummary(c.result.current.Context) {
				return false, nil
			}
			return true, writeContextCurrent(r.config.Out, c.result.current, boolValue(c.command.Flags(), "short"))
		},
	}
}

func renderContextList() commandRenderer {
	return commandRenderer{
		structured: structuredShown(func(result commandResult) (jsonResult, []diagnostic, bool) {
			if !validContextList(result.list) {
				return nil, nil, false
			}
			rows := slices.Clone(result.list.Contexts)
			slices.SortStableFunc(rows, func(a, b contexts.Summary) int { return strings.Compare(a.Name, b.Name) })
			list := contextListPresentation{Contexts: []contextSummaryPresentation{}}
			for _, row := range rows {
				list.Contexts = append(list.Contexts, contextSummary(row))
			}
			return list, nil, true
		}),
		human: func(r *Runner, c renderCall) (bool, error) {
			if !validContextList(c.result.list) {
				return false, nil
			}
			return true, writeContextList(r.config.Out, c.result.list)
		},
	}
}

func validContextDeletion(result *contexts.DeleteResult) bool {
	return result != nil && result.Name != "" && result.Outcome == "deleted"
}

func renderContextDelete() commandRenderer {
	return commandRenderer{
		structured: structuredShown(func(result commandResult) (jsonResult, []diagnostic, bool) {
			if !validContextDeletion(result.deletion) {
				return nil, nil, false
			}
			d := result.deletion
			shown := contextDeletionPresentation{Name: escapeDisplayLine(d.Name), Outcome: escapeDisplayLine(d.Outcome), CurrentCleared: d.CurrentCleared, OrphansAbandoned: d.OrphansAbandoned, ReleasedReservations: displayLines(d.ReleasedReservations)}
			var reported []diagnostic
			if d.OrphansAbandoned {
				reported = []diagnostic{{Severity: "warning", Code: "context.orphaned", Message: "the objects this context owned were abandoned and are no longer managed"}}
			}
			return shown, reported, true
		}),
		human: func(r *Runner, c renderCall) (bool, error) {
			if !validContextDeletion(c.result.deletion) {
				return false, nil
			}
			return true, writeContextDelete(r.config.Out, r.config.ErrOut, c.result.deletion)
		},
	}
}

func renderAddOnsList() commandRenderer {
	return commandRenderer{human: func(r *Runner, c renderCall) (bool, error) {
		if c.result.addOns == nil {
			return false, nil
		}
		return true, writeAddOns(r.config.Out, c.path, c.result.addOns, selectedStructured(c.command))
	}}
}

func renderAddOnsInfo() commandRenderer {
	return commandRenderer{human: func(r *Runner, c renderCall) (bool, error) {
		if c.result.addOnInfo == nil {
			return false, nil
		}
		return true, writeAddOnInfo(r.config.Out, c.path, c.result.addOnInfo, selectedStructured(c.command))
	}}
}

func renderSecretMutation(verb secretVerb) commandRenderer {
	return commandRenderer{
		structured: structuredShown(func(result commandResult) (jsonResult, []diagnostic, bool) {
			if !validSecretMutation(verb, result.secretMutation) {
				return nil, nil, false
			}
			m := result.secretMutation
			return secretMutationPresentation{Context: displaySecretContext(m.Context), Name: escapeDisplayLine(m.Name), Changed: m.Changed, Unchanged: m.Unchanged, ChangedNames: displayNames(m.ChangedNames), UnchangedNames: displayNames(m.UnchangedNames), Parts: displaySecretPartValues(m.Parts)}, nil, true
		}),
		human: func(r *Runner, c renderCall) (bool, error) {
			if !validSecretMutation(verb, c.result.secretMutation) {
				return false, nil
			}
			return true, writeSecretMutation(r.config.Out, verb, c.result.secretMutation)
		},
	}
}

func renderSecretCheck() commandRenderer {
	return commandRenderer{
		human: func(r *Runner, c renderCall) (bool, error) {
			if !availableSecretCheck(c.result.secretCheck) {
				return false, nil
			}
			return true, writeSecretCheck(r.config.Out, r.config.ErrOut, c.path, c.result.secretCheck, nil, 0, selectedJSON(c.command))
		},
		failed: func(r *Runner, c renderCall, reported []diagnostic) (bool, error) {
			if !negativeSecretCheck(c.result.secretCheck) {
				return false, nil
			}
			return true, writeSecretCheck(r.config.Out, r.config.ErrOut, c.path, c.result.secretCheck, reported, 1, selectedJSON(c.command))
		},
	}
}

func renderSecretInfo() commandRenderer {
	return commandRenderer{human: func(r *Runner, c renderCall) (bool, error) {
		if !validSecretList(c.result.secretList) {
			return false, nil
		}
		return true, writeSecretInfo(r.config.Out, c.path, c.result.secretList, selectedStructured(c.command))
	}}
}

func renderSecretList() commandRenderer {
	return commandRenderer{human: func(r *Runner, c renderCall) (bool, error) {
		if !validSecretList(c.result.secretList) {
			return false, nil
		}
		return true, writeSecretList(r.config.Out, c.path, c.result.secretList, selectedJSON(c.command))
	}}
}

func renderSecretShow() commandRenderer {
	return commandRenderer{
		structured: structuredShown(func(result commandResult) (jsonResult, []diagnostic, bool) {
			reveal := result.secretReveal
			if reveal == nil || !validSecretPart(reveal.Part) {
				return nil, nil, false
			}
			value, present := reveal.Material.Part(reveal.Part)
			defer clear(value)
			if !present || len(value) > secrets.MaxPartBytes || reveal.Material.Size() > secrets.MaxVersionBytes {
				return nil, nil, false
			}
			return exportedResult("application/octet-stream", value), nil, true
		}),
		human: func(r *Runner, c renderCall) (bool, error) {
			part := secrets.Part(stringValue(c.command.Flags(), "part"))
			if c.result.secretReveal != nil && c.result.secretReveal.Part == part && validSecretPart(part) {
				return true, writeSecretReveal(r.config.Out, c.result.secretReveal, part)
			}
			return false, nil
		},
	}
}

func renderEncryptionMutation(rotation bool) commandRenderer {
	return commandRenderer{
		structured: structuredShown(func(result commandResult) (jsonResult, []diagnostic, bool) {
			if !validEncryptionMutation(result.encryptionMutation) {
				return nil, nil, false
			}
			m := result.encryptionMutation
			i := m.Implementation
			return encryptionMutationPresentation{Context: displaySecretContext(m.Context), Implementation: encryptionSelectionPresentation{
				Type: escapeDisplayLine(i.Type), Store: componentStatus{ID: escapeDisplayLine(i.Store.ID), InterfaceVersion: i.Store.InterfaceVersion, StateVersion: i.Store.StateVersion, ConfigVersion: i.Store.ConfigVersion},
				KeyCustody: componentStatus{ID: escapeDisplayLine(i.KeyCustody.ID), InterfaceVersion: i.KeyCustody.InterfaceVersion, StateVersion: i.KeyCustody.StateVersion, ConfigVersion: i.KeyCustody.ConfigVersion},
			}, ActiveKey: escapeDisplayLine(m.ActiveKey), Changed: m.Changed, RetiredKeys: displayNames(m.RetiredKeys), ReencryptedVersions: m.ReencryptedVersions, ReencryptedParts: m.ReencryptedParts}, nil, true
		}),
		human: func(r *Runner, c renderCall) (bool, error) {
			if !validEncryptionMutation(c.result.encryptionMutation) {
				return false, nil
			}
			return true, writeEncryptionMutation(r.config.Out, rotation, c.result.encryptionMutation)
		},
	}
}

func renderEncryptionStatus() commandRenderer {
	return commandRenderer{human: func(r *Runner, c renderCall) (bool, error) {
		if !validEncryptionStatus(c.result.encryptionStatus) {
			return false, nil
		}
		return true, writeEncryptionStatus(r.config.Out, c.path, c.result.encryptionStatus, selectedJSON(c.command))
	}}
}

func renderMediaMutation() commandRenderer {
	return commandRenderer{
		structured: structuredShown(func(result commandResult) (jsonResult, []diagnostic, bool) {
			if !validMediaMutation(result.mediaMutation) {
				return nil, nil, false
			}
			m := result.mediaMutation
			return mediaMutationPresentation{Name: escapeDisplayLine(m.Name), Outcome: escapeDisplayLine(m.Outcome), Size: m.Size, SHA256: escapeDisplayLine(m.SHA256)}, nil, true
		}),
		human: func(r *Runner, c renderCall) (bool, error) {
			if !validMediaMutation(c.result.mediaMutation) {
				return false, nil
			}
			return true, writeMediaMutation(r.config.Out, c.result.mediaMutation)
		},
	}
}

func renderMediaList() commandRenderer {
	return commandRenderer{human: func(r *Runner, c renderCall) (bool, error) {
		if c.result.mediaList == nil {
			return false, nil
		}
		return true, writeMediaList(r.config.Out, c.path, c.result.mediaList, selectedJSON(c.command))
	}}
}
