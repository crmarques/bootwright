package lifecycle

import "sync"

type Derivations struct {
	mutex   sync.Mutex
	derived map[string]any
}

func NewDerivations() *Derivations { return &Derivations{derived: map[string]any{}} }

func Derived[T any](input PlanInput, key string, derive func() (T, error)) (T, error) {
	if input.Derived == nil {
		return derive()
	}
	input.Derived.mutex.Lock()
	defer input.Derived.mutex.Unlock()
	if known, found := input.Derived.derived[key]; found {
		return known.(T), nil
	}
	value, err := derive()
	if err != nil {
		return value, err
	}
	input.Derived.derived[key] = value
	return value, nil
}
