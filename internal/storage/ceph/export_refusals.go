package ceph

import (
	"github.com/crmarques/bootwright/internal/reconciliation"
)

type observedEffect struct {
	effect reconciliation.EffectState
	error
}

func (o observedEffect) WithDetail(string) error { return o }

func consumerRefusals() map[string]error {
	out := adapterRefusals()
	out["export-absent"] = observedEffect{reconciliation.EffectNoEffect, cephConsumerIncomplete.Fail("Ceph consumer created nothing yet", "")}
	out["export-incomplete"] = observedEffect{reconciliation.EffectPartial, cephConsumerIncomplete.Fail("Ceph consumer began and did not complete", "")}
	out["export-foreign-principal"] = cephForeign.Fail("Ceph consumer refused: export-foreign-principal", "inspect the consumer prerequisites and its owned identities before continuing")
	out["export-prerequisite"] = cephPrerequisite.Fail("Ceph consumer refused: export-prerequisite", "inspect the consumer prerequisites and its owned identities before continuing")
	out["export-identity"] = cephDrifted.Fail("Ceph consumer refused: export-identity", "inspect the consumer prerequisites and its owned identities before continuing")
	for reason, check := range map[string][2]string{
		"pool-identity":      {"the RBD pool's owned identity does not match the native pool", "restore the declared RBD pool and its original owned ID"},
		"rbd-application":    {"the declared pool lacks the native RBD application", "restore the rbd application on the declared owned pool"},
		"filesystem":         {"the filesystem's owned identity does not match the native filesystem", "restore the declared filesystem and its original owned ID"},
		"csi-group":          {"the declared filesystem lacks the csi subvolume group", "declare and realize the csi subvolume group on the consumer's filesystem"},
		"prometheus":         {"the MGR has not published a usable Prometheus HTTP endpoint", "restore the declared Prometheus service and its MGR endpoint"},
		"monitor-leader":     {"the selected MON leader is absent or not uniquely identified", "restore the frozen MON leader and its unique native identity"},
		"monitor-v2":         {"the MON v2 endpoint differs from the frozen address on port 3300", "restore the selected MON's declared address and v2 listener on port 3300"},
		"monitoring-address": {"the monitoring endpoint differs from its frozen hosts or port", "restore the declared monitoring placement and port"},
		"realm":              {"the gateway realm name or owned ID differs from native state", "restore the declared gateway realm and its original owned ID"},
		"zonegroup":          {"the gateway zone group name or owned ID differs from native state", "restore the declared gateway zone group and its original owned ID"},
		"zone":               {"the gateway zone name or owned ID differs from native state", "restore the declared gateway zone and its original owned ID"},
		"gateway-query":      {"radosgw-admin could not read the gateway prerequisites", "restore read access through the qualified radosgw-admin command and retry"},
	} {
		out["export-prerequisite-"+reason] = cephPrerequisite.Fail("Ceph consumer prerequisite: "+check[0], check[1]+" before retrying the original operation")
	}
	out["export-key-type"] = cephPrerequisite.Fail("the MON allowed CephX key types could not be proved", "inspect the MON auth_allowed_ciphers; Bootwright accepts aes and aes256k only and never changes the cluster cipher policy")
	for _, allowed := range []string{"aes", "aes256k"} {
		out["export-key-type-"+allowed] = cephPrerequisite.Fail("the MON allows only the CephX key type "+allowed+", which differs from the consumer declaration", "select spec.dataFoundation.cephxKeyType: "+allowed+" only when the consuming clients support it; Bootwright never changes the cluster cipher policy")
	}
	return out
}
