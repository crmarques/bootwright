//go:build linux && amd64

package selectionfs

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

// A refusal raised under the selection account reaches the elevated command
// with its repair, so it names the same repair an unelevated command does; a
// repair that is not one bounded printable line, or that follows an
// unclassified refusal, is dropped.
func TestAnElevatedSelectionRefusalKeepsItsRepair(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("helper deliberately refuses root; credential-drop test covers this under root")
	}
	const (
		file         = "selection file ~/.bootwright/context has an unsafe owner, type, link count or mode"
		fileRepair   = "remove it and select again with bootwright context use --name <context>"
		directory    = "~/.bootwright has an unsafe owner, type or mode"
		directoryFix = "make ~/.bootwright a directory you own with mode 0700"
	)
	expect := func(t *testing.T, err error, message, remediation string) {
		t.Helper()
		reported := diagnostics.Of(err)
		if len(reported) != 1 || reported[0].Code != "context.state" || reported[0].Message != message || reported[0].Remediation != remediation {
			t.Fatalf("got %#v, want %q with repair %q", reported, message, remediation)
		}
	}
	for name, row := range map[string]struct {
		damage               func(home string) error
		message, remediation string
	}{
		"marker mode":    {func(home string) error { return os.Chmod(filepath.Join(home, ".bootwright", "context"), 0644) }, file, fileRepair},
		"directory mode": {func(home string) error { return os.Chmod(filepath.Join(home, ".bootwright"), 0755) }, directory, directoryFix},
	} {
		t.Run(name, func(t *testing.T) {
			store, home := fixture(t)
			if err := store.Write(context.Background(), contexts.Selection{Version: contexts.SelectionVersion, Name: "lab"}); err != nil {
				t.Fatal(err)
			}
			if err := row.damage(home); err != nil {
				t.Fatal(err)
			}
			data, err := json.Marshal(helperRequest{Account: store.options, Action: "read"})
			if err != nil {
				t.Fatal(err)
			}
			var output bytes.Buffer
			if handled, code := ServeHelper(context.Background(), []string{HelperMode}, bytes.NewReader(data), &output); !handled || code != 0 {
				t.Fatalf("helper %v %d", handled, code)
			}
			_, err = decodeHelperResponse(output.Bytes(), false)
			expect(t, err, row.message, row.remediation)
		})
	}
	for name, row := range map[string]struct {
		response             helperResponse
		message, remediation string
	}{
		"repair kept":           {helperResponse{Failed: true, Message: file, Remediation: fileRepair}, file, fileRepair},
		"repair with a newline": {helperResponse{Failed: true, Message: file, Remediation: "remove it\nthen select"}, file, ""},
		"repair too long":       {helperResponse{Failed: true, Message: file, Remediation: strings.Repeat("x", 201)}, file, ""},
		"unclassified message":  {helperResponse{Failed: true, Message: strings.Repeat("x", 201), Remediation: fileRepair}, unclassifiedRefusal, ""},
	} {
		t.Run(name, func(t *testing.T) {
			data, err := json.Marshal(row.response)
			if err != nil {
				t.Fatal(err)
			}
			_, err = decodeHelperResponse(data, false)
			expect(t, err, row.message, row.remediation)
		})
	}
}
