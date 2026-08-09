package workflow

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
)

type addonStepResourcePool struct {
	mu      sync.Mutex
	held    map[string]bool
	changed chan struct{}
}

func newAddonStepResourcePool() *addonStepResourcePool {
	return &addonStepResourcePool{held: map[string]bool{}, changed: make(chan struct{})}
}

func (p *addonStepResourcePool) acquire(ctx context.Context, keys []string) (func(), error) {
	keys, err := normalizedAddonStepResourceKeys(keys)
	if err != nil {
		return nil, err
	}
	if len(keys) == 0 {
		return func() {}, nil
	}
	if p == nil {
		return nil, fmt.Errorf("add-on step resource pool is unavailable for %s", strings.Join(keys, ", "))
	}
	for {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("wait for add-on step resource %s: %w", strings.Join(keys, ", "), err)
		}
		p.mu.Lock()
		available := true
		for _, key := range keys {
			if p.held[key] {
				available = false
				break
			}
		}
		if available {
			for _, key := range keys {
				p.held[key] = true
			}
			p.mu.Unlock()
			var once sync.Once
			return func() {
				once.Do(func() {
					p.mu.Lock()
					for _, key := range keys {
						delete(p.held, key)
					}
					close(p.changed)
					p.changed = make(chan struct{})
					p.mu.Unlock()
				})
			}, nil
		}
		changed := p.changed
		p.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("wait for add-on step resource %s: %w", strings.Join(keys, ", "), ctx.Err())
		case <-changed:
		}
	}
}

func normalizedAddonStepResourceKeys(keys []string) ([]string, error) {
	out := append([]string(nil), keys...)
	for i, key := range out {
		out[i] = strings.TrimSpace(key)
		if out[i] == "" {
			return nil, fmt.Errorf("add-on step resource key is empty")
		}
	}
	sort.Strings(out)
	deduped := out[:0]
	for _, key := range out {
		if len(deduped) == 0 || deduped[len(deduped)-1] != key {
			deduped = append(deduped, key)
		}
	}
	return deduped, nil
}
