package ceph

import (
	"fmt"
	"strings"
	"testing"
)

func batchTestOperations() []map[string]any {
	sensitive := operationWithIdempotency("object-gateway", "create-rgw-user", "rgw-user", "admin",
		"radosgw-admin", "user", "create", "--uid", "admin")
	sensitive["no_log"] = true
	stretchRule := operationWithIdempotency("topology", "create-crush-rule-stretch", cephIdempotencyStretchCrushRule, "stretch")
	stretchRule["structural"] = map[string]any{"failureDomain": "datacenter", "replicasPerFailureDomain": 2}
	internalPools := operationWithIdempotency("late-topology", "reconcile-stretch-internal-pools", cephIdempotencyStretchInternalPools, "stretch")
	internalPools["structural"] = map[string]any{"ruleName": "stretch", "size": 4, "minSize": 2, "poolPattern": "^[.]"}
	return []map[string]any{
		operationInPhase("topology", "set-public-network", "ceph", "config", "set", "global", "public_network", "10.0.0.0/24"),
		operationInPhase("topology", "set-election-strategy", "ceph", "mon", "set", "election_strategy", "connectivity"),
		stretchRule,
		operationWithIdempotency("storage", "create-pool-rbd", "ceph-pool", "rbd", "ceph", "osd", "pool", "create", "rbd"),
		operationInPhase("storage", "set-pool-size-rbd", "ceph", "osd", "pool", "set", "rbd", "size", "4"),
		operationWithIdempotency("storage", "create-cephfs-subvolumegroup-fs-grp", "cephfs-subvolumegroup", "fs/grp",
			"ceph", "fs", "subvolumegroup", "create", "fs", "grp"),
		sensitive,
		operationWithStdin("object-gateway", "apply-nfs-export-share", `{"pseudo":"/share"}`,
			"ceph", "nfs", "export", "apply", "nfs", "-i", "-"),
		operationInPhase("object-gateway", "set-rgw-config-a", "ceph", "config", "set", "client.rgw.a", "rgw_thread_pool_size", "512"),
		operationInPhase("object-gateway", "set-rgw-config-b", "ceph", "config", "set", "client.rgw.b", "rgw_thread_pool_size", "512"),
		internalPools,
	}
}

func TestCephOperationPlanBreaksBatchesAtUnbatchableOperations(t *testing.T) {
	ops := batchTestOperations()
	plan, files := cephOperationPlan(ops)

	var got []string
	for _, step := range plan {
		if batch, ok := step["batch"].(string); ok {
			got = append(got, fmt.Sprintf("%s:batch:%s", step["group"], batch))
			continue
		}
		index, ok := step["operation"].(int)
		if !ok {
			t.Fatalf("plan step is neither a batch nor an operation: %v", step)
		}
		got = append(got, fmt.Sprintf("%s:op:%s", step["group"], opString(ops[index], "name")))
	}
	want := []string{
		"main:batch:apply-ops-main-01.sh",
		"main:op:create-crush-rule-stretch",
		"main:batch:apply-ops-main-02.sh",
		"late:op:create-rgw-user",
		"late:op:apply-nfs-export-share",
		"late:batch:apply-ops-late-03.sh",
		"late:op:reconcile-stretch-internal-pools",
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("plan steps =\n  %v\nwant\n  %v", got, want)
	}

	scripts := map[string]string{}
	for _, file := range files {
		scripts[file["file"].(string)] = file["content"].(string)
	}
	if _, ok := scripts[CephBatchLibFile]; !ok {
		t.Fatalf("batch files must carry the helper library: %v", scripts)
	}
	if _, ok := scripts[CephBatchGuardFile]; !ok {
		t.Fatalf("batch files must carry the guard program: %v", scripts)
	}
	for name, script := range scripts {
		if !strings.HasPrefix(name, "apply-ops-") {
			continue
		}
		for _, forbidden := range []string{
			"create-rgw-user",
			"apply-nfs-export-share",
			"create-crush-rule-stretch",
			"reconcile-stretch-internal-pools",
			"BW_STDIN",
		} {
			if strings.Contains(script, forbidden) {
				t.Fatalf("batch %s must not carry the unbatchable operation %q:\n%s", name, forbidden, script)
			}
		}
	}
}

func TestCephOperationPlanKeepsLoneOperationsOutOfBatches(t *testing.T) {
	ops := []map[string]any{
		operationInPhase("topology", "set-public-network", "ceph", "config", "set", "global", "public_network", "10.0.0.0/24"),
	}
	plan, files := cephOperationPlan(ops)
	if len(files) != 0 {
		t.Fatalf("a single batchable operation must not be staged as a batch, got %v", files)
	}
	if len(plan) != 1 || plan[0]["operation"] != 0 {
		t.Fatalf("plan = %v, want the lone operation as an operation step", plan)
	}
}

func TestCephBatchScriptGuardsKnownKindsAndRunsUnknownKindsUnguarded(t *testing.T) {
	_, files := cephOperationPlan(batchTestOperations())
	var pools string
	for _, file := range files {
		if file["file"] == "apply-ops-main-02.sh" {
			pools = file["content"].(string)
		}
	}
	if pools == "" {
		t.Fatalf("expected a staged pool batch, got %v", files)
	}
	if !strings.Contains(pools, "bw_batch_guarded create-pool-rbd ceph-pool rbd ceph osd pool create rbd") {
		t.Fatalf("guarded operation must carry its idempotency kind and resource:\n%s", pools)
	}
	if !strings.Contains(pools, "bw_batch_op set-pool-size-rbd ceph osd pool set rbd size 4") {
		t.Fatalf("unguarded operation must run through bw_batch_op:\n%s", pools)
	}
	if !strings.Contains(pools, "bw_batch_op create-cephfs-subvolumegroup-fs-grp ceph fs subvolumegroup create fs grp") {
		t.Fatalf("an idempotency kind the batch cannot probe must run unguarded, exactly as the per-operation ansible path does:\n%s", pools)
	}
	if !strings.Contains(pools, "source /mnt/"+CephBatchLibFile) {
		t.Fatalf("batch must source the staged helper library from the mounted work dir:\n%s", pools)
	}
	if !strings.Contains(pools, "BOOTWRIGHT_CEPH_BATCH_COMPLETE apply-ops-main-02.sh") {
		t.Fatalf("batch must report completion:\n%s", pools)
	}
}

func TestCephBatchLibNeverSnapshotsStretchIdempotency(t *testing.T) {
	lib := cephOperationBatchLib()
	for _, forbidden := range []string{
		cephIdempotencyStretchCrushRule,
		cephIdempotencyStretchInternalPools,
		"crushtool",
		"cephadm",
		"getcrushmap",
		"rule_id",
		"pool ls detail",
		"jq",
	} {
		if strings.Contains(lib, forbidden) {
			t.Fatalf("the batch helper library must not reach for %q: the stretch CRUSH rule id and the internal-pool reconcile both need a live read the batch cannot hold, and jq is not shipped in the Ceph container:\n%s", forbidden, lib)
		}
	}
	if !strings.Contains(lib, "python3 /mnt/"+CephBatchGuardFile) {
		t.Fatalf("the batch guard must run through the staged python3 program:\n%s", lib)
	}
	for kind := range cephBatchGuards() {
		if !strings.Contains(lib, "    "+kind+")") {
			t.Fatalf("batch helper library missing existence probe for idempotency kind %q:\n%s", kind, lib)
		}
	}
}
