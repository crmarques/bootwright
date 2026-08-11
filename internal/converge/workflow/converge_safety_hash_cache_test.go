package workflow

import (
	"sync"
	"testing"
)

func hashCacheTask() ApplyTask {
	return ApplyTask{
		Entry: TaskLedgerEntry{
			ID:           "addon.demo.a",
			Kind:         ApplyTaskKindClusterAddon,
			Label:        "addon a",
			Cluster:      "demo",
			ClusterKind:  ApplyClusterKindContainer,
			ResourceKeys: []string{"pullsecret:demo"},
			Status:       TaskStatusPending,
		},
		State: minimalState(),
	}
}

func TestApplyTaskHashCacheMatchesUncachedHash(t *testing.T) {
	task := hashCacheTask()
	want, err := computeApplyTaskDesiredHash(task)
	if err != nil {
		t.Fatalf("uncached desired hash: %v", err)
	}
	cached := task
	got, err := (&cached).DesiredHash()
	if err != nil {
		t.Fatalf("cached desired hash: %v", err)
	}
	if got != want {
		t.Fatalf("cached desired hash %s != uncached %s; the cache must never change a persisted converge-safety hash", got, want)
	}
	again, err := ApplyTaskDesiredHash(cached)
	if err != nil {
		t.Fatalf("value-call desired hash: %v", err)
	}
	if again != want {
		t.Fatalf("value call over a warm cache returned %s, want %s", again, want)
	}
	structural, err := (&cached).StructuralHash()
	if err != nil {
		t.Fatalf("cached structural hash: %v", err)
	}
	if structural != "" {
		t.Fatalf("an add-on task declares no structural vars, so its structural hash must stay empty, got %s", structural)
	}
}

func TestApplyTaskHashCacheIsInvalidatedByHashedFieldEdits(t *testing.T) {
	task := hashCacheTask()
	warm, err := (&task).DesiredHash()
	if err != nil {
		t.Fatalf("desired hash: %v", err)
	}
	cases := map[string]func(*ApplyTask){
		"resource keys": func(t *ApplyTask) { t.Entry.ResourceKeys = append(t.Entry.ResourceKeys, "pullsecret:other") },
		"task id":       func(t *ApplyTask) { t.Entry.ID = "addon.demo.b" },
		"playbook":      func(t *ApplyTask) { t.Playbook = "other.playbook" },
		"limit":         func(t *ApplyTask) { t.Limit = "other-host" },
		"node":          func(t *ApplyTask) { t.Entry.Node = "node-1" },
	}
	for name, mutate := range cases {
		copied := task
		mutate(&copied)
		got, err := ApplyTaskDesiredHash(copied)
		if err != nil {
			t.Fatalf("%s: desired hash: %v", name, err)
		}
		if got == warm {
			t.Fatalf("%s: a copy that edits a hashed field reused the original task's cached hash; converge safety would compare the wrong desired state", name)
		}
		fresh, err := computeApplyTaskDesiredHash(copied)
		if err != nil {
			t.Fatalf("%s: uncached desired hash: %v", name, err)
		}
		if got != fresh {
			t.Fatalf("%s: cached hash %s != uncached %s", name, got, fresh)
		}
	}
}

func TestApplyTaskHashCacheIsConcurrencySafe(t *testing.T) {
	task := hashCacheTask()
	want, err := computeApplyTaskDesiredHash(task)
	if err != nil {
		t.Fatalf("uncached desired hash: %v", err)
	}
	shared := task
	(&shared).hashCache()
	var wg sync.WaitGroup
	results := make([]string, 8)
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			copied := shared
			got, err := ApplyTaskDesiredHash(copied)
			if err != nil {
				t.Errorf("desired hash: %v", err)
				return
			}
			results[i] = got
		}(i)
	}
	wg.Wait()
	for i, got := range results {
		if got != want {
			t.Fatalf("goroutine %d saw desired hash %s, want %s", i, got, want)
		}
	}
}

func TestClassifyApplyObjectsWarmsTheTaskHashCache(t *testing.T) {
	runsDir := t.TempDir()
	tasks := []ApplyTask{hashCacheTask()}
	if _, err := ClassifyApplyObjects(tasks, runsDir, "test"); err != nil {
		t.Fatalf("ClassifyApplyObjects: %v", err)
	}
	if tasks[0].hashes == nil {
		t.Fatal("the classify pass must warm each task's hash cache so the run-time converge-safety mark does not recompute it")
	}
	cached, err := ApplyTaskDesiredHash(tasks[0])
	if err != nil {
		t.Fatalf("desired hash: %v", err)
	}
	fresh, err := computeApplyTaskDesiredHash(hashCacheTask())
	if err != nil {
		t.Fatalf("uncached desired hash: %v", err)
	}
	if cached != fresh {
		t.Fatalf("warmed hash %s != freshly computed %s", cached, fresh)
	}
}
