package prerequisites

import "strings"

// adapterCondition is what one classified adapter refusal states and the
// correction that settles it. A condition that reads a host is completed
// with the host the refused source names, and one that reads a filesystem
// with the area the refused source was being written into.
type adapterCondition struct {
	condition, correction string
	routable, native      bool
}

// adapterConditions is the closed vocabulary a controller adapter names its
// refusal by: the acquisition classes, which name the source being acquired,
// then the native helper's. internal is absent, so the caller keeps its own
// generic failure for it.
var adapterConditions = map[string]adapterCondition{
	"dns":               {condition: "%s could not be resolved by this host's configured resolver", correction: "Make %s resolvable", routable: true},
	"certificate":       {condition: "%s presented a certificate the qualified system trust store does not accept", correction: "Install the required certificate authority in the system trust store"},
	"unreachable":       {condition: "%s could not be reached from this host", correction: "Restore this host's access to %s", routable: true},
	"proxy":             {condition: "the HTTPS proxy refused or failed the connection to %s", correction: "Restore this host's access to %s", routable: true},
	"status":            {condition: "%s answered the approved download with an unexpected response", correction: "Restore the approved download at %s"},
	"redirect":          {condition: "%s redirected the approved download outside the approved publishers", correction: "Restore the approved download at %s"},
	"integrity":         {condition: "the download from %s differs from its approved size or digest", correction: "Restore the approved download at %s"},
	"trust":             {condition: "this host's system trust store /etc/pki/ca-trust/extracted/pem/tls-ca-bundle.pem is unreadable or unsafe", correction: "Restore it as a root-owned file that is not group- or world-writable with update-ca-trust"},
	"storage":           {condition: "the filesystem holding %a could not hold the approved download from %s", correction: "Free space on the filesystem holding %a"},
	"solver-conflict":   {condition: "the native package solver found the requested dependencies in conflict with this host's installed packages", correction: "Resolve the conflict with dnf", native: true},
	"missing-candidate": {condition: "the approved repositories offer no package satisfying a requested dependency version", correction: "Enable a repository that offers the requested dependency version", native: true},
	"signature":         {condition: "a native package failed its vendor signature check", correction: "Restore the vendor's signed packages and their keys", native: true},
	"database":          {condition: "the native package database was busy or changed during the operation", correction: "Wait for the other package operation to finish", native: true},
	"transaction":       {condition: "a vendor package script failed the native transaction", correction: "Resolve the failed package script with dnf", native: true},
	"postcondition":     {condition: "the host's package inventory differs from the frozen plan", correction: "Hold other package changes until setup finishes", native: true},
	"foundation":        {condition: "the provided OS Python and DNF foundation is incomplete", correction: "Restore the provided OS package-manager foundation", native: true},
}

// AdapterRefusal maps the class a controller adapter's refused record names
// to its diagnostic and remedy. host is the URL host of the source an
// acquisition refusal names, and area the directory that source was being
// written into, since a package stages in the run's scratch while a tool is
// written into its publication bundle; both are empty for a native refusal,
// which is how a
// timeout is told apart: a bounded transfer, or the native inspection. A
// refusal after Go authorized a native transaction leaves that transaction's
// outcome unproved, so it is unknown. internal, or a class outside the
// vocabulary, is not mapped, and the caller keeps its generic failure. No
// adapter text ever reaches the diagnostic.
func AdapterRefusal(reason, host, area string, authorized bool) (error, bool) {
	condition, known := adapterConditions[reason]
	if reason == "timeout" {
		condition, known = adapterCondition{condition: "%s did not answer within its bounded acquisition deadline", correction: "Restore this host's access to %s", routable: true}, true
		if host == "" {
			condition = adapterCondition{condition: "the native package inspection did not finish within its bound", correction: "Restore the provided OS package-manager foundation", native: true}
		}
	}
	if !known || condition.native != (host == "") || area == "" && strings.Contains(condition.condition, "%a") {
		return nil, false
	}
	failure := &ScopedFailure{Code: "controller.setup", Message: fill(condition.condition, host, area), Correction: fill(condition.correction, host, area), Routable: condition.routable}
	if authorized {
		failure.Code, failure.Correction, failure.Routable = "controller.unknown", "Resolve the native transaction", false
	}
	return failure, true
}

// fill puts host where a condition or correction reads %s, and area where it
// reads %a.
func fill(text, host, area string) string {
	return strings.ReplaceAll(strings.ReplaceAll(text, "%s", host), "%a", area)
}
