package repocheck

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStorageCephRetryPollsCarryAnAttemptEscape(t *testing.T) {
	root := repoRoot(t)
	base := filepath.Join(root, "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks")
	var offenders []string
	err := filepath.WalkDir(base, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || (filepath.Ext(path) != ".yml" && filepath.Ext(path) != ".yaml") {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		text := string(data)
		polls := strings.Count(text, "retries:")
		escapes := strings.Count(text, ".attempts | default(1) | int)")
		if polls > escapes {
			offenders = append(offenders, fmt.Sprintf("%s (%d retry poll(s), %d attempt escape(s))", rel, polls, escapes))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", base, err)
	}
	if len(offenders) > 0 {
		t.Fatalf("every retry poll in the storage_cluster_cephadm role must carry an attempt escape — `or ((<register>.attempts | default(1) | int) >= (<retries var> | int))`.\nansible-core resolves `failed_when` inside the retry loop body and then, in the loop's else branch, sets result['failed'] = True unconditionally when the budget runs out (task_executor.py, \"we ran out of attempts\"), so `failed_when: false` does NOT survive retry exhaustion. The storage apply play is any_errors_fatal, so an exhausted poll aborts the run with \"Ran out of attempts\" BEFORE the crafted diagnostic and refusal that follow it can render — and a diagnostic poll whose predicate is unsatisfiable in the very failure it diagnoses (an empty device inventory on a cluster cephadm never scanned) is exactly where that costs the operator the whole diagnosis.\noffenders:\n  %s", strings.Join(offenders, "\n  "))
	}
}
