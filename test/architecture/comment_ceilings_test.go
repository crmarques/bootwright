package architecture_test

// commentLineCeilings is the most comment lines each production Go package may
// hold, directives aside. Each ceiling only shrinks (D101): lower it when a
// package loses comments, never raise it.
func commentLineCeilings() map[string]int {
	return map[string]int{
		"ansible": 14, "api/v1alpha1": 53, "cmd/bootwright": 208,
		"internal/adapterprotocol": 132, "internal/addons": 10, "internal/addons/catalog": 0,
		"internal/addons/preflight": 0, "internal/availability": 0, "internal/canonicaljson": 27,
		"internal/cli": 450, "internal/containercluster": 29, "internal/containercluster/access": 24,
		"internal/containercluster/agentinstall": 467, "internal/containercluster/installation": 0,
		"internal/containercluster/preflight": 0, "internal/controller": 125,
		"internal/controller/ansiblelocal": 91, "internal/controller/bundlelocal": 316,
		"internal/controller/clients": 134, "internal/controller/hostdnf": 1, "internal/controller/hostlinux": 24,
		"internal/controller/prerequisites": 524, "internal/controller/privilege": 245, "internal/controller/runtimelock": 32,
		"internal/controller/stagelocal": 74, "internal/controller/venvlocal": 45,
		"internal/desiredstate": 0, "internal/desiredstate/compilation": 52,
		"internal/desiredstate/customplaybooks": 5, "internal/desiredstate/encoding": 2,
		"internal/desiredstate/inputfs": 38, "internal/desiredstate/yamlstream": 3,
		"internal/diagnostics": 2, "internal/environment": 6, "internal/environment/access": 0,
		"internal/environment/inspection": 0, "internal/environment/preflight": 0,
		"internal/infrastructureservices": 55, "internal/infrastructureservices/artifactserver": 182,
		"internal/infrastructureservices/dnsserver": 5, "internal/infrastructureservices/managedservice": 192,
		"internal/infrastructureservices/ntpserver": 3, "internal/infrastructureservices/proxy": 8,
		"internal/machine": 166, "internal/machine/access": 84, "internal/machine/inventory": 48,
		"internal/machine/power": 203, "internal/machine/sshlocal": 94, "internal/managedos": 56,
		"internal/managedos/installation": 597, "internal/managedos/media": 198,
		"internal/managedos/media/storecontract": 16, "internal/managedos/medialocal": 43,
		"internal/nativeartifacts/rendering": 0, "internal/reconciliation": 139,
		"internal/reconciliation/ansiblerunner": 213, "internal/reconciliation/contextguard": 8,
		"internal/reconciliation/lifecycle": 1790, "internal/reconciliation/lifecycle/workspacecontract": 20,
		"internal/reconciliation/operationstore": 287, "internal/reconciliation/operationstore/areacontract": 12,
		"internal/reconciliation/operationstore/areadouble": 16, "internal/secrets": 54,
		"internal/secrets/custody": 81, "internal/secrets/encryption": 11, "internal/secrets/localkeyring": 26,
		"internal/secrets/material": 53, "internal/secrets/secretstore": 61,
		"internal/secrets/secretstore/areacontract": 10, "internal/storage": 4, "internal/storage/ceph": 17, "internal/storage/preflight": 0,
		"internal/storage/rendering": 0, "internal/substrate": 291, "internal/substrate/baremetal": 103,
		"internal/substrate/libvirt": 319, "internal/trust": 62, "internal/trust/enrollment": 57,
		"internal/workspace/contextfs": 856, "internal/workspace/contexts": 131,
		"internal/workspace/invokerfs": 48, "internal/workspace/selectionfs": 32,
	}
}
