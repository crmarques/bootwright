package ceph

import (
	"fmt"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

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
