package installation

import (
	"regexp"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/managedos"
)

const (
	UpdateRecordPath    = "/etc/bootwright/update.json"
	ReleaseVariablePath = "/etc/dnf/vars/releasever"
)

const (
	updateNone     = "none"
	updateAbsent   = "absent"
	updatePending  = "pending"
	updateUpgraded = "upgraded"
	updateComplete = "complete"
	updateForeign  = "foreign"
)

var (
	updateRelease = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
	updateKernel  = regexp.MustCompile(`^[A-Za-z0-9._+~-]{1,128}$`)
	updateFound   = regexp.MustCompile(`^[A-Za-z0-9._+~-]{1,64}$`)
	updateCause   = map[string][]string{
		"update-unavailable": {"metadata", "download", "timeout"},
		"update-failed":      {"resolution", "transaction", "timeout", "history", "tooling"},
	}
)

func updateFor(profile api.Object) (*Update, error) {
	if !managedos.UpdatesWithinRelease(profile) {
		return nil, nil
	}
	release := profile.Spec().Get("os", "version").Text()
	if !updateRelease.MatchString(release) {
		return nil, refusal("lifecycle.unsupported", profile.Identity()+" updates within release "+release+
			", which is not a <major>.<minor> release such as 9.8",
			"set spec.os.version on "+profile.Identity()+" to the exact release, or set spec.updates.policy to none on "+profile.Identity())
	}
	return &Update{Policy: managedos.UpdateWithinRelease, Release: release}, nil
}

// updateRefusal is completed by the worker's detail; without one the value reads unknown.
type updateRefusal struct {
	render func(detail string) error
}

func (r updateRefusal) Error() string                  { return r.render("").Error() }
func (r updateRefusal) Unwrap() error                  { return r.render("") }
func (r updateRefusal) WithDetail(detail string) error { return r.render(detail) }

func detailOr(detail string, valid func(string) bool) string {
	if valid(detail) {
		return detail
	}
	return "unknown"
}

func driftDetail(detail string) (found, repository string) {
	found, repository, ok := strings.Cut(detail, "@")
	if !ok || !updateFound.MatchString(found) || !managedos.RepositoryID(repository) {
		return "unknown", "unknown"
	}
	return found, repository
}

func updateRefusals(contextName string, request Request) map[string]error {
	if request.Update == nil {
		return nil
	}
	machine := "Machine/" + request.Identity.Object
	profile := "MachineInstallProfile/" + request.Identity.Profile
	release := request.Update.Release
	refused := machineRefusal(request)
	again := "then run bootwright destroy --context " + contextName + " and apply again"
	fixed := func(message, remediation string) error {
		return updateRefusal{render: func(string) error { return refused(message, remediation) }}
	}
	caused := func(reason, message, remediation string) error {
		return updateRefusal{render: func(detail string) error {
			cause := detailOr(detail, func(value string) bool {
				for _, known := range updateCause[reason] {
					if value == known {
						return true
					}
				}
				return false
			})
			return refused(message+cause, remediation)
		}}
	}
	return map[string]error{
		"update-release-unavailable": fixed("the subscription service offers no release "+release+" to "+machine,
			"set spec.os.version on "+profile+" to a release the subscription service offers, or set spec.updates.policy to none, "+again),
		"update-release-mismatch": updateRefusal{render: func(detail string) error {
			return refused(machine+" reports VERSION_ID "+detailOr(detail, updateFound.MatchString)+", and its installation declares release "+release,
				"run bootwright destroy --context "+contextName+", make the update sources of "+profile+" offer release "+release+" alone, and apply again")
		}},
		"update-release-drift": updateRefusal{render: func(detail string) error {
			found, repository := driftDetail(detail)
			return refused("the update of "+machine+" would move redhat-release to "+found+" from repository "+repository+", outside release "+release,
				"pin repository "+repository+" of "+profile+" to release "+release+" or disable it, "+again)
		}},
		"update-no-repository": fixed(machine+" has no enabled repository to update release "+release+" from",
			"enable a repository for "+profile+" or set spec.updates.policy to none, "+again),
		"update-foreign": fixed(machine+" holds an update record of another installation",
			"run bootwright destroy --context "+contextName+" and apply again"),
		"update-unavailable": caused("update-unavailable", "a repository did not answer while "+machine+" updated within release "+release+": ",
			"restore the route from "+machine+" to its update sources, then run bootwright apply --context "+contextName+" again"),
		"update-failed": caused("update-failed", "the package transaction of "+machine+" failed while it updated within release "+release+": ",
			"run bootwright apply --context "+contextName+" again once the update sources of "+profile+
				" offer a consistent transaction, or run bootwright destroy --context "+contextName+" and set spec.updates.policy to none"),
	}
}
