package contexts

import (
	"errors"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

// AbsentContext refuses a named context the store does not hold, whether the
// name was given or read from the selection.
func AbsentContext(name string) error {
	return diagnostics.NewFailureWithRemediation("context.state", "context "+name+" does not exist", "",
		"create it with bootwright context init --name "+name+", or select an existing one with bootwright context use --name <context>; bootwright context list names them")
}

func NoSelection() error {
	return diagnostics.NewFailureWithRemediation("context.state", "no current context is selected", "",
		"select one with bootwright context use --name <context>, or create one with bootwright context init --name <context>")
}

func MissingInput(name string) error {
	return diagnostics.NewFailureWithRemediation("context.input", "context "+name+" has no desired state", "",
		"import it with bootwright context update --name "+name+" --input-dir <dir>")
}

// NotReady refuses a context an init or a deletion left unfinished, naming
// the command that finishes it.
func NotReady(record Record) error {
	if record.Mode == Initializing {
		return diagnostics.NewFailureWithRemediation("context.state", "context "+record.Name+" is still initializing: an earlier context init did not finish", "",
			"finish it with bootwright context init --name "+record.Name+" and its original Context file, from any input directory, or discard it with bootwright context delete --name "+record.Name+" --purge")
	}
	return diagnostics.NewFailureWithRemediation("context.state", "context "+record.Name+" is being deleted", "",
		"finish the deletion with bootwright context delete --name "+record.Name+" --purge")
}

// AlreadyExists refuses an init over a name the store holds; an initializing
// name is the init's own to resume, so it never reaches this refusal.
func AlreadyExists(record Record) error {
	if record.Mode == Deleting {
		return diagnostics.NewFailureWithRemediation("context.state", "context "+record.Name+" is being deleted", "",
			"finish the deletion with bootwright context delete --name "+record.Name+" --purge, then repeat the init")
	}
	return diagnostics.NewFailureWithRemediation("context.state", "context "+record.Name+" already exists", "",
		"replace its input with bootwright context update --name "+record.Name+" --input-dir <dir>, or delete it with bootwright context delete --name "+record.Name+" --purge")
}

// IncompleteOperation refuses an update while an operation is incomplete. Its
// destroy is named outright, because it plans from the frozen plan and so
// still runs when status cannot compile the stored input.
func IncompleteOperation(name string) error {
	return diagnostics.NewFailureWithRemediation("context.state", "context "+name+" has an incomplete operation, which continues from the input it froze", "",
		"continue it with the command bootwright status --context "+name+" names, or take back what it owns with bootwright destroy --context "+name+", then repeat the update")
}

// absentWhenEmpty reports a store that holds no context yet as the absence of
// the one name the command was asked for.
func absentWhenEmpty(err error, name string) error {
	if errors.Is(err, ErrNoContexts) {
		return AbsentContext(name)
	}
	return err
}

// selectionNotUpdated keeps the selection store's own diagnosis of a created
// context it could not select.
func selectionNotUpdated(name string, err error) error {
	cause := err.Error()
	if reported := diagnostics.Of(err); len(reported) != 0 {
		cause = reported[0].Message
	}
	return diagnostics.NewFailureWithRemediation("context.state", "context "+name+" was created, but the current selection could not be updated: "+cause, "",
		"select it with bootwright context use --name "+name)
}

// reshapeFailure rewrites each diagnostic a refusal carries and keeps every
// other error as it is.
func reshapeFailure(err error, change func(*diagnostics.Diagnostic)) error {
	var failure *diagnostics.Failure
	if !errors.As(err, &failure) {
		return err
	}
	reported := diagnostics.Of(err)
	for index := range reported {
		change(&reported[index])
	}
	return &diagnostics.Failure{Diagnostics: reported, Usage: failure.Usage}
}

// ErrNoContexts marks a store that holds no context yet. A repository's
// refusal matches it with errors.Is and reports the NoContexts diagnostic; a
// caller that knows the name it was asked for reports AbsentContext for that
// name instead.
var ErrNoContexts = errors.New("no context exists on this host yet")

func NoContexts() error {
	return diagnostics.NewFailureWithRemediation("context.state", "no context exists on this host yet", "",
		"create one with bootwright context init --name <name>")
}

// ErrUnreadableEvidence marks the guard's refusal of mutation evidence it
// cannot read: missing, corrupt or unsupported. A guard's refusal matches it
// with errors.Is. Nothing then proves what the context owns, so only the
// explicit orphan acknowledgement deletes it, and it cannot list what it
// abandons.
var ErrUnreadableEvidence = errors.New("context mutation evidence is missing, corrupt or unsupported")

// unreadableEvidenceRefusal is an update's or a default deletion's refusal
// over evidence the guard cannot read, naming the one exit that remains.
func unreadableEvidenceRefusal(code, name string) error {
	return diagnostics.NewFailureWithRemediation(code,
		"the mutation evidence of context "+name+" is missing, corrupt or unsupported, so nothing proves what it owns", "",
		"review its records with bootwright status --context "+name+", then abandon whatever it owns with bootwright context delete --name "+name+" --purge --allow-orphans")
}

// orphanRefusal is a default deletion's refusal of a context whose evidence
// still attributes objects to it, naming status as their inventory.
func orphanRefusal(name string) error {
	return UnsafeDeleteWithRemediation("context "+name+" still owns realized objects; deleting it would orphan them",
		"review them with bootwright status --context "+name+", then remove them with bootwright destroy --context "+name+
			", or abandon them with bootwright context delete --name "+name+" --purge --allow-orphans")
}

// appliedInputWarning is the warning of changed input over a completed apply,
// which apply refuses until a destroy takes back what that apply owns.
func appliedInputWarning(name string) diagnostics.Diagnostic {
	return diagnostics.Diagnostic{
		Severity: "warning", Code: "lifecycle.state",
		Message:     "context " + name + " holds a completed apply, and apply refuses changed desired state until what that apply owns is taken back",
		Remediation: "take it back with bootwright destroy --context " + name + ", then run bootwright apply --context " + name,
	}
}
