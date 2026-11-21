package machine

import (
	"errors"
	"testing"
)

func TestParseCopyPathsDistinguishesMissingFromInvalidOperands(t *testing.T) {
	for _, test := range []struct {
		name     string
		operands []string
		missing  bool
	}{
		{name: "none", missing: true},
		{name: "valid source only", operands: []string{"local:source"}, missing: true},
		{name: "invalid source only", operands: []string{"local:"}},
		{name: "same side", operands: []string{"remote:source", "remote:destination"}},
		{name: "extra operand", operands: []string{"local:source", "remote:destination", "extra"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, _, err := ParseCopyPaths(test.operands)
			if err == nil || errors.Is(err, ErrMissingCopyOperand) != test.missing {
				t.Fatalf("operands=%q error=%v, want missing=%t", test.operands, err, test.missing)
			}
		})
	}

	source, destination, err := ParseCopyPaths([]string{"remote:/source", "local:destination"})
	if err != nil || source != (CopyPath{Remote: true, Path: "/source"}) || destination != (CopyPath{Path: "destination"}) {
		t.Fatalf("source=%+v destination=%+v error=%v", source, destination, err)
	}
}
