package diagnostics

const usageCode = "cli.usage"

var defaultRemediations = map[string]string{
	"input.not-found":     "correct the input path and repeat the command",
	"input.not-directory": "pass a directory as the input and repeat the command",
	"input.symlink":       "replace the symbolic link with the regular file or directory it names and repeat the command",
	"input.read":          "make the input readable by the invoking user and repeat the command",
	"input.limit":         "reduce the input to within the documented limit and repeat the command",
	"yaml.syntax":         "correct the YAML at the reported source location and repeat the command",
	"yaml.duplicate-key":  "remove the repeated key at the reported source location",
	"yaml.alias":          "write the value out instead of using an anchor, alias or merge key",
	"yaml.tag":            "remove the YAML tag at the reported source location",
	"yaml.shape":          "write each document as a mapping with string keys",
	"api.version":         "set apiVersion to a version this build supports",
	"api.kind":            "set kind to a kind this build supports",
	"api.field":           "remove the unknown field or correct its spelling",
	"api.type":            "write the field with the type its schema documents",
	"api.required":        "add the required field",
	"api.value":           "write a value the field's schema permits",
	"api.duplicate":       "remove or rename the duplicated object or value",
	"api.reference":       "declare the referenced object or correct the reference",
	"api.invariant":       "change one of the fields the message names so the rule holds",
	"api.selection":       "select the Environment that includes the object, or remove the object",

	"cli.not-implemented": "run bootwright help <command> to see which forms this build runs",
	"runtime.encode":      "repeat the command; a repeated failure is a Bootwright defect to report",
	"runtime.log":         "make the context's log directory writable by the invoking user and repeat the command",
	"runtime.interrupted": "repeat the command",
	"runtime.canceled":    "repeat the command",
	"runtime.deadline":    "repeat the command",
	"runtime.privilege":   "run bootwright as root, or as a user whose sudo rule permits it, and repeat the command",
	"runtime.internal":    "repeat the command; a repeated failure is a Bootwright defect to report",

	"context.configuration": "correct the context setup the message names and repeat the command",
	"context.input":         "import a desired-state revision with bootwright context update --name <context> --input-dir <dir>",
	"context.state":         "run bootwright context list to see the contexts",
	"context.unsafe-delete": "resolve the evidence the message names and repeat the deletion",
	"context.orphaned":      "review the abandoned objects the message names",

	"secret.store":                 "run bootwright secret check to see which Secrets are usable",
	"secret.declaration":           "correct the Secret name or declaration in the desired state",
	"secret.source":                "correct the Secret's declared source or parameters",
	"secret.part":                  "supply the Secret part the message names, or correct its value",
	"secret.input":                 "supply valid Secret input or material and repeat the command",
	"secret.store.uninitialized":   "initialize the Secret store with bootwright secret encryption init",
	"secret.store.implementation":  "configure the Secret store implementation the message names",
	"secret.store.key-unavailable": "make the Secret store key or unlock session available and repeat the command",
	"secret.store.conflict":        "repeat the command once the concurrent change has finished",
	"secret.store.corrupt":         "if a newer Bootwright build used this store, run that build, because the store is then intact; otherwise restore the Secret store state the message names from a backup",
	"secret.store.newer":           "run the Bootwright build that wrote the Secret store, or a newer one; the store is intact, so do not restore or purge it",
	"secret.store.crypto":          "repeat the command; if it fails again the host's randomness or key material is unusable",
	"secret.store.limit":           "reduce the Secret material, input or metadata to within the bound the message names",

	"media.store":       "run bootwright media list to see the stored media",
	"preflight.failed":  "resolve the failed checks and repeat the preflight",
	"preflight.unknown": "resolve the unknown and pending checks and repeat the preflight",

	"controller.unsupported": "run setup on a qualified host, or choose a supported dependency combination or route",
	"controller.identity":    "run bootwright setup to record the controller binding and host evidence",
	"controller.state":       "run bootwright setup to replace the retained setup evidence",
	"controller.conflict":    "wait for the other setup or context to finish and repeat the command",
	"controller.setup":       "resolve the failed action the message names and repeat setup",
	"controller.unknown":     "repeat bootwright setup to resolve the unknown action",

	"lifecycle.state":           "run bootwright status to see the records and the next step",
	"lifecycle.unsupported":     "remove the object this executable cannot realize, or use a build that supports it",
	"lifecycle.stage":           "select a stage that has a startable block",
	"lifecycle.authorization":   "repeat the command with the authorization the frozen plan requires",
	"lifecycle.lease":           "repeat the command once the other operation on the context has finished",
	"lifecycle.unknown":         "resolve the unknown effect with the command bootwright status names",
	"lifecycle.live":            "stop what still uses the state and repeat the removal",
	"lifecycle.adapter-running": "wait for the adapter the message names to end and repeat the command",

	"trust.identity":     "review the SSH identity the message names and enroll it with bootwright machine trust",
	"access.unavailable": "provide the access metadata or credential the message names and repeat the command",
	"access.target":      "name exactly one authorized target and repeat the command",
	"access.handoff":     "correct the access descriptor the message names and repeat the command",
	"access.credential":  "reveal the Machine's password credential and repeat the command",

	"cluster.not-applicable": "run this command against a cluster of a kind it applies to",
	"machine.power":          "resolve the refusal the message names and repeat the power command",
}

func DefaultRemediation(code string) string { return defaultRemediations[code] }

func WithRemediation(d Diagnostic) Diagnostic {
	if d.Severity == "error" && d.Code != usageCode && d.Remediation == "" {
		d.Remediation = DefaultRemediation(d.Code)
	}
	return d
}

func DefaultRemediationCodes() []string {
	codes := make([]string, 0, len(defaultRemediations))
	for code := range defaultRemediations {
		codes = append(codes, code)
	}
	return codes
}
