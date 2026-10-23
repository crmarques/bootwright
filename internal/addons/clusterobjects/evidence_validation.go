package clusterobjects

import (
	"fmt"
	"regexp"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

var minorVersion = regexp.MustCompile(`^[0-9]+\.[0-9]+$`)

func ValidateEvidence(evidence Evidence) error {
	fail := func() error { return fmt.Errorf("cluster object evidence contains an invalid native value") }
	for _, receipt := range evidence.Receipts {
		if validateRef(receipt.Ref) != nil || !bounded(receipt.UID) || !bounded(receipt.DesiredDigest) {
			return fail()
		}
	}
	for _, object := range evidence.Observations {
		if validateRef(object.Ref) != nil {
			return fail()
		}
	}
	for _, check := range evidence.Checks {
		if !bounded(check.LastObservation) {
			return fail()
		}
	}
	for _, olm := range evidence.OLM {
		if len(olm.Plans) > 16 || len(olm.CSVs) > 16 {
			return fail()
		}
		for _, plan := range olm.Plans {
			if !nativeName(plan.Name) || len(plan.CSVs) > 16 {
				return fail()
			}
			for _, csv := range plan.CSVs {
				if !nativeName(csv) {
					return fail()
				}
			}
		}
		for _, csv := range olm.CSVs {
			if !nativeName(csv.Name) || !bounded(csv.Version) || !bounded(csv.Phase) {
				return fail()
			}
		}
		if !nativeName(olm.Namespace) || !nativeName(olm.Subscription) || !nativeName(olm.InstalledCSV) || !bounded(olm.CSVVersion) || !nativeImage(olm.CatalogImage) || !nativeImage(olm.BundleImage) || !validPlan(olm.InstallPlan) {
			return fail()
		}
		if olm.MaxOpenShiftVersion != "" && olm.MaxOpenShiftVersion != "invalid" && (!bounded(olm.MaxOpenShiftVersion) || !minorVersion.MatchString(olm.MaxOpenShiftVersion)) {
			return fail()
		}
	}
	for _, dependent := range evidence.Dependents {
		if !nativeName(dependent.Namespace) || !nativeName(dependent.Subscription) || !nativeName(dependent.InstalledCSV) || !bounded(dependent.UID) || !validPlan(dependent.InstallPlan) {
			return fail()
		}
	}
	return nil
}
func bounded(value string) bool    { return value == BoundedObservation(value) }
func nativeName(value string) bool { return value == "" || bounded(value) && validName(value) }
func nativeImage(value string) bool {
	return value == "" || value == "invalid" || bounded(value) && api.ValidLexical("image", value)
}
func validPlan(plan InstallPlan) bool {
	if !nativeName(plan.Name) || !bounded(plan.UID) {
		return false
	}
	for _, csv := range plan.CSVs {
		if !nativeName(csv) {
			return false
		}
	}
	return true
}
