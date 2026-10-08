package lifecycle

import "testing"

// Bound Secret material an attempt could not read names the exact command
// that reopens the operation's bindings: an apply's continuation, which every
// run of that attempt carries, or, when the next command depends on what the
// whole run settles, status's next command.
func TestABindingRefusalNamesTheCommandThatReopensIt(t *testing.T) {
	const continuation = "bootwright apply --context lab --authorize data-loss"
	if got, want := ReopenBindings("lab", continuation), "repeat "+continuation+", which reopens this operation's Secret bindings"; got != want {
		t.Fatalf("ReopenBindings = %q, want %q", got, want)
	}
	if got, want := ReopenBindings("lab", ""), "run bootwright status --context lab and repeat the command it names, which reopens this operation's Secret bindings"; got != want {
		t.Fatalf("ReopenBindings = %q, want %q", got, want)
	}
	if got := RunFor(Execution{Context: "lab", Continuation: continuation}, Invocation{}).Repeat; got != continuation {
		t.Fatalf("the run repeats %q, want %q", got, continuation)
	}
	if got := RunFor(Execution{Context: "lab"}, Invocation{}).Repeat; got != "" {
		t.Fatalf("a run without a continuation repeats %q", got)
	}
}
