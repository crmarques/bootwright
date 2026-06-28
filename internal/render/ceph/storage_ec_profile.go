package ceph

import (
	"fmt"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

// erasureProfileOperation renders the create-ec-profile op for an erasure-coded
// pool, carrying a live-comparable structural identity. The EC profile is
// immutable in Ceph and cannot be deleted while a pool uses it, so its --override
// rebuild runs at this op (which precedes the pool create): the cephadm role
// compares these authored fields to the live profile and, on a mismatch, tears
// down the one dependent pool and the stale profile so this op recreates the
// profile and the create-pool op recreates the pool.
func erasureProfileOperation(poolName, profile string, profileCmd []string, failureDomain string, ec *v1alpha1.StoragePoolErasureCode) map[string]any {
	op := operationWithIdempotency("storage", "create-ec-profile-"+poolName, "ec-profile", profile, profileCmd...)
	op["structural"] = map[string]any{
		"kind":    "ec-profile",
		"pool":    poolName,
		"profile": profile,
		"fields":  storageECProfileStructuralFields(ec, failureDomain),
	}
	return op
}

// storageECProfileStructuralFields is the live-comparable identity of an
// erasure-code profile under --override: the authored fields, keyed by their
// `ceph osd erasure-code-profile get` JSON spellings and stringified to match the
// string-valued live profile. The role rebuilds the profile (and its one
// dependent pool, data-destroying) only when a field here differs from the live
// profile; Ceph's defaulted keys are absent here and so are ignored.
func storageECProfileStructuralFields(ec *v1alpha1.StoragePoolErasureCode, failureDomain string) map[string]string {
	fields := map[string]string{
		"k":                    fmt.Sprintf("%d", ec.DataChunks),
		"m":                    fmt.Sprintf("%d", ec.CodingChunks),
		"crush-failure-domain": failureDomain,
	}
	if ec.Plugin != "" {
		fields["plugin"] = ec.Plugin
	}
	if ec.Technique != "" {
		fields["technique"] = ec.Technique
	}
	if ec.CrushDeviceClass != "" {
		fields["crush-device-class"] = ec.CrushDeviceClass
	}
	if ec.CrushRoot != "" {
		fields["crush-root"] = ec.CrushRoot
	}
	if ec.StripeUnit != "" {
		fields["stripe_unit"] = ec.StripeUnit
	}
	for key, value := range ec.Parameters {
		fields[key] = value
	}
	return fields
}
