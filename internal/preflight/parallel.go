package preflight

import "sync"

const (
	nameResolutionParallelism = 8
	packageSourceParallelism  = 8
	vCenterParallelism        = 8
)

func forEachBounded(count, limit int, fn func(int)) {
	if count <= 0 {
		return
	}
	if limit < 1 {
		limit = 1
	}
	if count == 1 || limit == 1 {
		for i := 0; i < count; i++ {
			fn(i)
		}
		return
	}
	slots := make(chan struct{}, limit)
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		wg.Add(1)
		slots <- struct{}{}
		go func(index int) {
			defer wg.Done()
			defer func() { <-slots }()
			fn(index)
		}(i)
	}
	wg.Wait()
}
