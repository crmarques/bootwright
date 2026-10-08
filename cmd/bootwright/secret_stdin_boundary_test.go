package main

import (
	"bytes"
	"context"
	"testing"

	"github.com/crmarques/bootwright/internal/controller/privilege"
)

// The privilege boundary hands the elevator the Secret a set reads from
// standard input, so a sudo policy that logs input refuses it before the child
// starts; a file-input set names none.
func TestTheBoundaryNamesTheStandardInputSecretToTheElevator(t *testing.T) {
	for _, test := range []struct {
		args []string
		want string
	}{
		{args: []string{"secret", "set", "--context", "lab", "--name", "bmc", "--value-stdin"}, want: "bmc"},
		{args: []string{"secret", "set", "--context", "lab", "--name", "bmc", "--username", "admin", "--password-stdin"}, want: "bmc"},
		{args: []string{"secret", "set", "--context", "lab", "--name", "bmc", "--value-file", "value"}, want: ""},
	} {
		var elevated []privilege.Invocation
		var stdout, stderr bytes.Buffer
		code := boundaryUnderTest(nil, privilege.Outcome{}, &elevated).run(context.Background(), test.args, &stdout, &stderr)
		if code != 0 || len(elevated) != 1 {
			t.Fatalf("%v: exit %d, elevated %d times, stderr %q", test.args, code, len(elevated), stderr.String())
		}
		if elevated[0].SecretStdin != test.want {
			t.Fatalf("%v: the elevator was handed SecretStdin %q, want %q", test.args, elevated[0].SecretStdin, test.want)
		}
	}
}
