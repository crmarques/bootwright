package ceph

import (
	"fmt"
	"strings"

	"github.com/crmarques/bootwright/internal/host/shellquote"
)

const (
	CephBatchMountRoot = "/mnt"
	CephBatchLibFile   = "ceph-batch-lib.sh"
	CephBatchGuardFile = "ceph-batch-guard.py"

	cephBatchGroupMain = "main"
	cephBatchGroupLate = "late"

	cephBatchMinimumOperations = 2

	cephIdempotencyStretchCrushRule     = "stretch-crush-rule"
	cephIdempotencyStretchInternalPools = "stretch-internal-pools"
)

type cephBatchGuard struct {
	mode  string
	key   string
	probe []string
}

func cephBatchGuards() map[string]cephBatchGuard {
	return map[string]cephBatchGuard{
		"ceph-pool":    {mode: "string-list", probe: []string{"ceph", "osd", "pool", "ls", "--format", "json"}},
		"ec-profile":   {mode: "string-list", probe: []string{"ceph", "osd", "erasure-code-profile", "ls", "--format", "json"}},
		"cephfs":       {mode: "attr-list", key: "name", probe: []string{"ceph", "fs", "ls", "--format", "json"}},
		"crush-rule":   {mode: "attr-list", key: "rule_name", probe: []string{"ceph", "osd", "crush", "rule", "dump", "--format", "json"}},
		"mgr-module":   {mode: "mgr-module", probe: []string{"ceph", "mgr", "module", "ls", "--format", "json"}},
		"stretch-mode": {mode: "flag-true", key: "stretch_mode", probe: []string{"ceph", "mon", "dump", "--format", "json"}},
		"rgw-realm":    {mode: "key-list", key: "realms", probe: []string{"radosgw-admin", "realm", "list", "--format", "json"}},
		"rgw-zonegroup": {
			mode: "key-list", key: "zonegroups",
			probe: []string{"radosgw-admin", "zonegroup", "list", "--format", "json"},
		},
		"rgw-zone": {mode: "key-list", key: "zones", probe: []string{"radosgw-admin", "zone", "list", "--format", "json"}},
	}
}

func cephOperationBatchGroup(phase string) (string, bool) {
	switch phase {
	case "", "topology", "storage":
		return cephBatchGroupMain, true
	case "object-gateway", "late-topology":
		return cephBatchGroupLate, true
	}
	return "", false
}

func cephOperationBatchable(op map[string]any) bool {
	if len(opCommand(op)) == 0 || opString(op, "stdin") != "" || opSensitive(op) {
		return false
	}
	switch kind, _, _ := opIdempotency(op); kind {
	case cephIdempotencyStretchCrushRule, cephIdempotencyStretchInternalPools:
		return false
	}
	return true
}

func cephOperationPlan(ops []map[string]any) (plan []map[string]any, files []map[string]any) {
	var batches []map[string]any
	for _, group := range []string{cephBatchGroupMain, cephBatchGroupLate} {
		var pending []map[string]any
		flush := func() {
			if len(pending) < cephBatchMinimumOperations {
				for _, entry := range pending {
					plan = append(plan, map[string]any{"group": group, "operation": entry["index"]})
				}
				pending = nil
				return
			}
			file := fmt.Sprintf("apply-ops-%s-%02d.sh", group, len(batches)+1)
			names := make([]string, 0, len(pending))
			members := make([]map[string]any, 0, len(pending))
			for _, entry := range pending {
				op, _ := entry["op"].(map[string]any)
				names = append(names, opString(op, "name"))
				members = append(members, op)
			}
			batches = append(batches, map[string]any{
				"file":    file,
				"content": cephBatchScript(file, members),
			})
			plan = append(plan, map[string]any{"group": group, "batch": file, "operations": names})
			pending = nil
		}
		for index, op := range ops {
			opGroup, ok := cephOperationBatchGroup(opString(op, "phase"))
			if !ok || opGroup != group {
				continue
			}
			if cephOperationBatchable(op) {
				pending = append(pending, map[string]any{"index": index, "op": op})
				continue
			}
			flush()
			plan = append(plan, map[string]any{"group": group, "operation": index})
		}
		flush()
	}
	if len(batches) == 0 {
		return plan, nil
	}
	files = append(files, map[string]any{"file": CephBatchLibFile, "content": cephOperationBatchLib()})
	files = append(files, map[string]any{"file": CephBatchGuardFile, "content": cephOperationBatchGuard()})
	return plan, append(files, batches...)
}

func cephBatchScript(file string, ops []map[string]any) string {
	var b strings.Builder
	b.WriteString("#!/usr/bin/env bash\n")
	b.WriteString("set -uo pipefail\n")
	b.WriteString("# shellcheck source=/dev/null\n")
	fmt.Fprintf(&b, "source %s/%s\n", CephBatchMountRoot, CephBatchLibFile)
	guards := cephBatchGuards()
	for _, op := range ops {
		name := opString(op, "name")
		cmd := opCommand(op)
		kind, resource, guarded := opIdempotency(op)
		if _, ok := guards[kind]; guarded && ok {
			fmt.Fprintf(&b, "bw_batch_guarded %s\n", shellquote.QuoteWords(append([]string{name, kind, resource}, cmd...)))
			continue
		}
		fmt.Fprintf(&b, "bw_batch_op %s\n", shellquote.QuoteWords(append([]string{name}, cmd...)))
	}
	fmt.Fprintf(&b, "echo %s\n", shellquote.QuoteWord("BOOTWRIGHT_CEPH_BATCH_COMPLETE "+file))
	return b.String()
}

func cephOperationBatchLib() string {
	var b strings.Builder
	b.WriteString("#!/usr/bin/env bash\n")
	b.WriteString("set -uo pipefail\n\n")
	b.WriteString("bw_batch_op() {\n")
	b.WriteString("  local name=\"$1\"\n")
	b.WriteString("  local rc=0\n")
	b.WriteString("  shift\n")
	b.WriteString("  echo \"BOOTWRIGHT_CEPH_OP ${name}\"\n")
	b.WriteString("  \"$@\" || rc=$?\n")
	b.WriteString("  if [ \"${rc}\" -ne 0 ]; then\n")
	b.WriteString("    echo \"BOOTWRIGHT_CEPH_OP_FAILED ${name}\" >&2\n")
	b.WriteString("    exit \"${rc}\"\n")
	b.WriteString("  fi\n")
	b.WriteString("}\n\n")
	b.WriteString("bw_batch_guarded() {\n")
	b.WriteString("  local name=\"$1\" kind=\"$2\" resource=\"$3\"\n")
	b.WriteString("  shift 3\n")
	b.WriteString("  if _bw_batch_exists \"${kind}\" \"${resource}\"; then\n")
	b.WriteString("    echo \"BOOTWRIGHT_CEPH_OP_SKIPPED ${name}\"\n")
	b.WriteString("    return 0\n")
	b.WriteString("  fi\n")
	b.WriteString("  bw_batch_op \"${name}\" \"$@\"\n")
	b.WriteString("}\n\n")
	b.WriteString("_bw_batch_probe() {\n")
	b.WriteString("  \"$@\" 2>/dev/null || true\n")
	b.WriteString("}\n\n")
	fmt.Fprintf(&b, "_bw_batch_match() {\n  python3 %s/%s \"$1\" \"$2\" \"$3\"\n}\n\n", CephBatchMountRoot, CephBatchGuardFile)
	b.WriteString("_bw_batch_exists() {\n")
	b.WriteString("  local kind=\"$1\" name=\"$2\"\n")
	b.WriteString("  case \"${kind}\" in\n")
	guards := cephBatchGuards()
	for _, kind := range sortedKeys(guards) {
		guard := guards[kind]
		fmt.Fprintf(&b, "    %s)\n", kind)
		fmt.Fprintf(&b, "      _bw_batch_probe %s \\\n", shellquote.QuoteWords(guard.probe))
		fmt.Fprintf(&b, "        | _bw_batch_match %s %s \"${name}\" ;;\n",
			shellquote.QuoteWord(guard.mode), shellquote.QuoteWord(guard.key))
	}
	b.WriteString("    *)\n")
	b.WriteString("      return 1 ;;\n")
	b.WriteString("  esac\n")
	b.WriteString("}\n")
	return b.String()
}

func cephOperationBatchGuard() string {
	return cephBatchGuardProgram
}

const cephBatchGuardProgram = `import json
import sys

mode = sys.argv[1]
key = sys.argv[2]
name = sys.argv[3]
raw = sys.stdin.read().strip()
try:
    doc = json.loads(raw) if raw else None
except ValueError:
    doc = None


def strings(value):
    out = []
    if isinstance(value, list):
        for item in value:
            if isinstance(item, str):
                out.append(item)
    return out


def truthy(value):
    if isinstance(value, bool):
        return value
    if isinstance(value, int):
        return value == 1
    if isinstance(value, str):
        return value.strip().lower() in ("true", "yes", "1")
    return False


found = False
if mode == "string-list":
    found = name in strings(doc)
elif mode == "attr-list":
    if isinstance(doc, list):
        for item in doc:
            if isinstance(item, dict) and item.get(key) == name:
                found = True
elif mode == "key-list":
    if isinstance(doc, dict):
        found = name in strings(doc.get(key))
elif mode == "mgr-module":
    if isinstance(doc, dict):
        modules = strings(doc.get("enabled_modules")) + strings(doc.get("always_on_modules"))
        found = name in modules
elif mode == "flag-true":
    found = isinstance(doc, dict) and truthy(doc.get(key))
else:
    sys.stderr.write("BOOTWRIGHT_CEPH_GUARD_UNKNOWN_MODE " + mode + "\n")
    sys.exit(2)

sys.exit(0 if found else 1)
`
