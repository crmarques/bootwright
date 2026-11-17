package fanout

import (
	"context"
	"fmt"
	"runtime/debug"
)

type Options[I, O any] struct {
	Limit    int
	Throttle int
	Key      func(I) string
	Started  func(index int)
	Settled  func(index int, outcome Outcome[O])
}

type Outcome[O any] struct {
	Value   O
	Err     error
	Started bool
}

type PanicError struct {
	Value any
	Stack []byte
}

func (e *PanicError) Error() string {
	return fmt.Sprintf("fan-out work panicked: %v", e.Value)
}

type finished[O any] struct {
	index    int
	outcome  Outcome[O]
	panicked *PanicError
}

func Run[I, O any](ctx context.Context, options Options[I, O], inputs []I, work func(context.Context, I) (O, error)) []Outcome[O] {
	outcomes := make([]Outcome[O], len(inputs))
	limit, throttle := max(options.Limit, 1), max(options.Throttle, 1)
	key := func(index int) string {
		if options.Key == nil {
			return ""
		}
		return options.Key(inputs[index])
	}
	settle := func(index int, outcome Outcome[O]) {
		outcomes[index] = outcome
		if options.Settled != nil {
			options.Settled(index, outcome)
		}
	}
	done := make(chan finished[O], len(inputs))
	waiting := make([]int, len(inputs))
	for index := range waiting {
		waiting[index] = index
	}
	running, held := 0, map[string]int{}
	ctx, cancel := context.WithCancel(ctx)
	defer func() {
		cancel()
		for ; running > 0; running-- {
			<-done
		}
	}()
	var panicked *PanicError
	for len(waiting) != 0 || running != 0 {
		if err := ctx.Err(); err != nil {
			for _, index := range waiting {
				settle(index, Outcome[O]{Err: err})
			}
			waiting = nil
		}
		admitted := waiting[:0]
		for _, index := range waiting {
			name := key(index)
			if ctx.Err() != nil || running >= limit || name != "" && held[name] >= throttle {
				admitted = append(admitted, index)
				continue
			}
			running++
			if name != "" {
				held[name]++
			}
			if options.Started != nil {
				options.Started(index)
			}
			go func() {
				value, failure, err := guarded(ctx, inputs[index], work)
				done <- finished[O]{index: index, outcome: Outcome[O]{Value: value, Err: err, Started: true}, panicked: failure}
			}()
		}
		waiting = admitted
		if running == 0 {
			continue
		}
		var next finished[O]
		select {
		case next = <-done:
		case <-ctx.Done():
			if len(waiting) != 0 {
				continue
			}
			next = <-done
		}
		running--
		if name := key(next.index); name != "" {
			held[name]--
		}
		if next.panicked != nil {
			if panicked == nil {
				panicked = next.panicked
				cancel()
			}
			waiting = nil
			continue
		}
		if panicked == nil {
			settle(next.index, next.outcome)
		}
	}
	if panicked != nil {
		panic(panicked)
	}
	return outcomes
}

func FirstError[O any](outcomes []Outcome[O]) error {
	for _, outcome := range outcomes {
		if outcome.Err != nil {
			return outcome.Err
		}
	}
	return nil
}

func guarded[I, O any](ctx context.Context, input I, work func(context.Context, I) (O, error)) (value O, failure *PanicError, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			var zero O
			value, failure, err = zero, &PanicError{Value: recovered, Stack: debug.Stack()}, nil
		}
	}()
	value, err = work(ctx, input)
	return value, nil, err
}
