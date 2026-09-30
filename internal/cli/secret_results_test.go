package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/secrets/custody"
	"github.com/crmarques/bootwright/internal/secrets/encryption"
	"github.com/crmarques/bootwright/internal/secrets/secretstore"
)

func secretResultContext() secretstore.Context {
	return secretstore.Context{Name: "example", Mode: "ready"}
}

func secretResultComponent(id string) secretstore.ComponentRef {
	return secretstore.ComponentRef{ID: id, InterfaceVersion: 1, StateVersion: 1, ConfigVersion: 1}
}

func runSecretResult(args []string, record *dispatchRecord) (int, string, string) {
	var out, errOut bytes.Buffer
	code := New(Config{Out: &out, ErrOut: &errOut, Services: dispatchSpies(record)}).Run(context.Background(), args)
	return code, out.String(), errOut.String()
}

func TestSecretCheckAndListResultsAreCanonical(t *testing.T) {
	version, generated := "ver-2", "ver-3"
	check := &custody.CheckResult{Context: secretResultContext(), Secrets: []custody.CheckRow{
		{Name: "zeta", Type: "sshKeyPair", Source: "generated", Parts: []secrets.Part{secrets.PublicKeyPart, secrets.PrivateKeyPart}, Status: "available", Version: &generated, Sequence: 1},
		{Name: "alpha", Type: "token", Source: "contextStore", Parts: []secrets.Part{secrets.ValuePart}, Status: "available", Version: &version, Sequence: 2},
	}}
	record := &dispatchRecord{result: commandResult{secretCheck: check}}
	code, out, errOut := runSecretResult([]string{"secret", "check", "--output", "json"}, record)
	want := "{\"schemaVersion\":\"v1alpha1\",\"command\":\"secret check\",\"ok\":true,\"exitCode\":0,\"result\":{\"context\":{\"name\":\"example\",\"mode\":\"ready\"},\"secrets\":[{\"name\":\"alpha\",\"type\":\"token\",\"source\":\"contextStore\",\"parts\":[\"value\"],\"status\":\"available\",\"version\":\"ver-2\",\"sequence\":2},{\"name\":\"zeta\",\"type\":\"sshKeyPair\",\"source\":\"generated\",\"parts\":[\"private-key\",\"public-key\"],\"status\":\"available\",\"version\":\"ver-3\",\"sequence\":1}]},\"diagnostics\":[],\"logs\":[]}\n"
	if code != 0 || out != want || errOut != "" || record.calls != 1 {
		t.Fatalf("check code=%d stdout=%q stderr=%q calls=%d", code, out, errOut, record.calls)
	}

	current := "ver-1"
	list := &custody.ListResult{Context: secretResultContext(), Secrets: []custody.ListRow{
		{Name: "zeta", Type: "caBundle", Source: "generated", Parts: []secrets.Part{secrets.PrivateKeyPart, secrets.CertificatePart}, State: "stale", CurrentVersion: &current, CurrentSequence: 1, BoundVersions: 2},
		{Name: "alpha", Type: "opaque", Source: "contextStore", Parts: []secrets.Part{secrets.ValuePart}, State: "orphaned", BoundVersions: 1},
	}}
	record = &dispatchRecord{result: commandResult{secretList: list}}
	code, out, errOut = runSecretResult([]string{"secret", "list", "--output", "json"}, record)
	want = "{\"schemaVersion\":\"v1alpha1\",\"command\":\"secret list\",\"ok\":true,\"exitCode\":0,\"result\":{\"context\":{\"name\":\"example\",\"mode\":\"ready\"},\"secrets\":[{\"name\":\"alpha\",\"type\":\"opaque\",\"source\":\"contextStore\",\"parts\":[\"value\"],\"state\":\"orphaned\",\"currentVersion\":null,\"currentSequence\":0,\"boundVersions\":1},{\"name\":\"zeta\",\"type\":\"caBundle\",\"source\":\"generated\",\"parts\":[\"certificate\",\"private-key\"],\"state\":\"stale\",\"currentVersion\":\"ver-1\",\"currentSequence\":1,\"boundVersions\":2}]},\"diagnostics\":[],\"logs\":[]}\n"
	if code != 0 || out != want || errOut != "" || record.calls != 1 {
		t.Fatalf("list code=%d stdout=%q stderr=%q calls=%d", code, out, errOut, record.calls)
	}
}

func TestCompleteNegativeSecretCheckKeepsStructuredResult(t *testing.T) {
	result := &custody.CheckResult{Context: secretResultContext(), Secrets: []custody.CheckRow{{Name: "token", Type: "token", Source: "contextStore", Parts: []secrets.Part{secrets.ValuePart}, Status: "missing"}}}
	failure := &diagnostics.Failure{Diagnostics: []diagnostics.Diagnostic{{Severity: "error", Code: "secret.input", Message: "secret token is missing"}}}
	for _, mode := range []string{"text", "json"} {
		record := &dispatchRecord{result: commandResult{secretCheck: result}, err: failure}
		code, out, errOut := runSecretResult([]string{"secret", "check", "--output", mode}, record)
		if code != 1 || record.calls != 1 {
			t.Fatalf("%s code=%d calls=%d", mode, code, record.calls)
		}
		if mode == "text" {
			if !strings.Contains(out, "token  token  contextStore  value  missing  -") || errOut != "[FAIL] secret.input: secret token is missing\n" {
				t.Fatalf("text stdout=%q stderr=%q", out, errOut)
			}
			continue
		}
		var envelope struct {
			OK          bool
			ExitCode    int
			Result      *custody.CheckResult
			Diagnostics []diagnostic
			Logs        []string
		}
		if err := json.Unmarshal([]byte(out), &envelope); err != nil || envelope.OK || envelope.ExitCode != 1 || envelope.Result == nil || len(envelope.Result.Secrets) != 1 || envelope.Result.Secrets[0].Status != "missing" || len(envelope.Diagnostics) != 1 || len(envelope.Logs) != 0 || errOut != "" || strings.Count(out, "\n") != 1 {
			t.Fatalf("json stdout=%q stderr=%q decode=%v", out, errOut, err)
		}
	}
}

func TestIncompleteNegativeSecretCheckDoesNotPublishPartialResult(t *testing.T) {
	partial := &custody.CheckResult{Secrets: []custody.CheckRow{{Name: "token", Type: "token", Source: "contextStore", Parts: []secrets.Part{secrets.ValuePart}, Status: "missing"}}}
	failure := diagnostics.NewFailure("secret.input", "safe failure", "")
	record := &dispatchRecord{result: commandResult{secretCheck: partial}, err: failure}
	code, out, errOut := runSecretResult([]string{"secret", "check", "--output", "json"}, record)
	if code != 1 || errOut != "" || strings.Contains(out, "token") || !strings.Contains(out, "\"result\":null") || !strings.Contains(out, "secret.input") {
		t.Fatalf("partial check code=%d stdout=%q stderr=%q", code, out, errOut)
	}
}

func TestNegativeSecretCheckCannotSucceedWithoutDiagnostics(t *testing.T) {
	result := &custody.CheckResult{Context: secretResultContext(), Secrets: []custody.CheckRow{{Name: "token", Type: "token", Source: "contextStore", Parts: []secrets.Part{secrets.ValuePart}, Status: "missing"}}}
	record := &dispatchRecord{result: commandResult{secretCheck: result}}
	code, out, errOut := runSecretResult([]string{"secret", "check"}, record)
	if code != 1 || out != "" || !strings.Contains(errOut, "runtime.internal") || strings.Contains(errOut, "token") {
		t.Fatalf("inconsistent check code=%d stdout=%q stderr=%q", code, out, errOut)
	}
}

type partialSecretWriter struct{ bytes.Buffer }

func (w *partialSecretWriter) Write(value []byte) (int, error) {
	count := len(value) / 2
	_, _ = w.Buffer.Write(value[:count])
	return count, nil
}

func TestSecretRevealIsExactClearedAndNeverFollowedByFallback(t *testing.T) {
	value := []byte{0, 'a', '\n', 0xff, 'z'}
	result := &custody.RevealResult{Material: secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: value}), Part: secrets.ValuePart}
	record := &dispatchRecord{result: commandResult{secretReveal: result}}
	var out, errOut bytes.Buffer
	code := New(Config{Out: &out, ErrOut: &errOut, Services: dispatchSpies(record)}).Run(context.Background(), []string{"secret", "show", "--name", "sample", "--part", "value"})
	if code != 0 || !bytes.Equal(out.Bytes(), value) || errOut.Len() != 0 {
		t.Fatalf("reveal code=%d stdout=%q stderr=%q", code, out.Bytes(), errOut.Bytes())
	}
	cleared, _ := result.Material.Part(secrets.ValuePart)
	if !bytes.Equal(cleared, make([]byte, len(value))) {
		t.Fatalf("result material was not cleared: %q", cleared)
	}

	result = &custody.RevealResult{Material: secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: value}), Part: secrets.ValuePart}
	record = &dispatchRecord{result: commandResult{secretReveal: result}}
	writer := &partialSecretWriter{}
	errOut.Reset()
	code = New(Config{Out: writer, ErrOut: &errOut, Services: dispatchSpies(record)}).Run(context.Background(), []string{"secret", "show", "--name", "sample", "--part", "value"})
	if code != 1 || !bytes.Equal(writer.Bytes(), value[:len(value)/2]) || errOut.Len() != 0 || strings.Contains(writer.String(), "FAIL") {
		t.Fatalf("partial reveal code=%d stdout=%q stderr=%q", code, writer.Bytes(), errOut.Bytes())
	}
	cleared, _ = result.Material.Part(secrets.ValuePart)
	if !bytes.Equal(cleared, make([]byte, len(value))) {
		t.Fatalf("failed result material was not cleared: %q", cleared)
	}
}

func TestSecretRevealDetectsShortWriteDirectly(t *testing.T) {
	value := []byte("exact-secret")
	result := &custody.RevealResult{Material: secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: value}), Part: secrets.ValuePart}
	defer result.Material.Clear()
	writer := &partialSecretWriter{}
	if err := writeSecretReveal(writer, result, secrets.ValuePart); !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("short write error = %v; want %v", err, io.ErrShortWrite)
	}
}

func TestSecretRevealRejectsWrongPartWithoutDisclosure(t *testing.T) {
	value := []byte("private material marker")
	result := &custody.RevealResult{Material: secrets.NewMaterial(map[secrets.Part][]byte{secrets.PasswordPart: value}), Part: secrets.PasswordPart}
	record := &dispatchRecord{result: commandResult{secretReveal: result}}
	code, out, errOut := runSecretResult([]string{"secret", "show", "--name", "sample", "--part", "value"}, record)
	if code != 1 || out != "" || !strings.Contains(errOut, "runtime.internal") || strings.Contains(errOut, "marker") {
		t.Fatalf("wrong part code=%d stdout=%q stderr=%q", code, out, errOut)
	}
	cleared, _ := result.Material.Part(secrets.PasswordPart)
	if !bytes.Equal(cleared, make([]byte, len(value))) {
		t.Fatal("rejected reveal material was not cleared")
	}
}

func TestSecretRevealMaterialIsClearedWhenServiceFailsOrIsInterrupted(t *testing.T) {
	value := []byte("confidential-marker")
	for _, interrupted := range []bool{false, true} {
		result := &custody.RevealResult{Material: secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: value}), Part: secrets.ValuePart}
		record := &dispatchRecord{result: commandResult{secretReveal: result}}
		var begin func(context.Context) (context.Context, func())
		if interrupted {
			var cancel context.CancelCauseFunc
			begin = func(ctx context.Context) (context.Context, func()) {
				ctx, cancel = context.WithCancelCause(ctx)
				return ctx, func() { cancel(nil) }
			}
			record.afterCall = func() { cancel(ErrInterrupted) }
		} else {
			record.err = diagnostics.NewFailure("secret.input", "reveal failed safely", "")
		}
		var out, errOut bytes.Buffer
		code := New(Config{Out: &out, ErrOut: &errOut, Services: dispatchSpies(record), BeginOperation: begin}).Run(context.Background(), []string{"secret", "show", "--name", "sample", "--part", "value"})
		if code != map[bool]int{false: 1, true: 130}[interrupted] || out.Len() != 0 || strings.Contains(errOut.String(), "confidential-marker") {
			t.Fatalf("interrupted=%t code=%d stdout=%q stderr=%q", interrupted, code, out.Bytes(), errOut.Bytes())
		}
		cleared, _ := result.Material.Part(secrets.ValuePart)
		if !bytes.Equal(cleared, make([]byte, len(value))) {
			t.Fatalf("interrupted=%t result material was not cleared", interrupted)
		}
	}
}

func TestSecretMutationAndEncryptionResults(t *testing.T) {
	mutation := &custody.MutationResult{Context: secretResultContext(), Name: "sample", Changed: 1, Parts: []secrets.Part{secrets.ValuePart}}
	record := &dispatchRecord{result: commandResult{secretMutation: mutation}}
	code, out, errOut := runSecretResult([]string{"secret", "set", "--name", "sample", "--value-file", "value"}, record)
	if code != 0 || !strings.Contains(out, "[OK] Secret set complete") || !strings.Contains(out, "Parts      value") || errOut != "" {
		t.Fatalf("mutation code=%d stdout=%q stderr=%q", code, out, errOut)
	}

	selection := secretstore.Selection{Type: "local-keyring", Store: secretResultComponent("local-v1"), KeyCustody: secretResultComponent("local-keyfile-v1")}
	encryptionMutation := &encryption.MutationResult{Context: secretResultContext(), Implementation: selection, ActiveKey: "key-1", Changed: true}
	record = &dispatchRecord{result: commandResult{encryptionMutation: encryptionMutation}}
	code, out, errOut = runSecretResult([]string{"secret", "encryption", "init"}, record)
	if code != 0 || !strings.Contains(out, "[OK] Secret encryption initialized") || !strings.Contains(out, "Type         local-keyring") || errOut != "" {
		t.Fatalf("encryption mutation code=%d stdout=%q stderr=%q", code, out, errOut)
	}
}

func TestEncryptionStatusOmitsImplementationConfiguration(t *testing.T) {
	active := "key-2"
	status := &encryption.StatusResult{
		Initialized: true,
		Implementation: &encryption.ImplementationStatus{
			Type:       "local-keyring",
			Store:      encryption.ComponentStatus{ID: "local-v1", InterfaceVersion: 1, StateVersion: 1, ConfigVersion: 1},
			KeyCustody: encryption.ComponentStatus{ID: "local-keyfile-v1", InterfaceVersion: 1, StateVersion: 1, ConfigVersion: 1},
			State:      "ready",
		},
		ActiveKey: &active,
		Keys:      []secretstore.Key{{ID: "key-2", State: "active", Seals: 3}, {ID: "key-1", State: "retired", Seals: 8}},
		Items:     encryption.ItemStatus{CurrentVersions: 2, BoundVersions: 1, MaterialParts: 4, RetainedArtifacts: 7, CleanupRequired: true},
	}
	record := &dispatchRecord{result: commandResult{encryptionStatus: status}}
	code, out, errOut := runSecretResult([]string{"secret", "encryption", "status", "--output", "json"}, record)
	orderedKeys := "\"keys\":[{\"id\":\"key-1\",\"state\":\"retired\",\"seals\":8},{\"id\":\"key-2\",\"state\":\"active\",\"seals\":3}]"
	if code != 0 || errOut != "" || strings.Contains(out, "\"config\"") || !strings.Contains(out, "\"implementation\":{\"type\":\"local-keyring\"") || !strings.Contains(out, orderedKeys) {
		t.Fatalf("status code=%d stdout=%q stderr=%q", code, out, errOut)
	}
}

// Display escapes each value once, so a backslash in the encryption status or
// in a secret's parts reads as the two JSON's safe display text carries, and
// the keys keep the order JSON gives them.
func TestSecretTextEscapesOnce(t *testing.T) {
	raw := func(label string) string { return label + `\x` }
	shown := func(label string) string { return label + `\\x` }
	active := raw("active-key")
	status := &encryption.StatusResult{
		Initialized: true,
		Implementation: &encryption.ImplementationStatus{
			Type:       raw("type"),
			Store:      encryption.ComponentStatus{ID: raw("store"), InterfaceVersion: 1, StateVersion: 1, ConfigVersion: 1},
			KeyCustody: encryption.ComponentStatus{ID: raw("custody"), InterfaceVersion: 1, StateVersion: 1, ConfigVersion: 1},
			State:      raw("state"),
		},
		ActiveKey: &active,
		Keys:      []secretstore.Key{{ID: raw("b-key"), State: raw("retired"), Seals: 8}, {ID: raw("a-key"), State: raw("active"), Seals: 3}},
	}
	var out bytes.Buffer
	if err := writeEncryptionStatus(&out, "secret encryption status", status, false); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	if strings.Contains(text, `\\\\`) {
		t.Fatalf("encryption status text escaped a value twice: %q", text)
	}
	for _, want := range []string{
		"  Implementation  " + shown("type") + "\n", "  State           " + shown("state") + "\n",
		"  Store           " + shown("store") + "\n", "  Key custody     " + shown("custody") + "\n",
		"  Active key      " + shown("active-key") + "\n",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("encryption status text = %q, missing %q", text, want)
		}
	}
	first := strings.Index(text, "  "+shown("a-key")+"  "+shown("active")+"   3\n")
	second := strings.Index(text, "  "+shown("b-key")+"  "+shown("retired")+"  8\n")
	if first < 0 || second < first {
		t.Fatalf("encryption status keys = %q, want %s then %s", text, shown("a-key"), shown("b-key"))
	}
	part := secrets.Part(raw("part"))
	var check, list, mutation bytes.Buffer
	checked := &custody.CheckResult{Context: secretResultContext(), Secrets: []custody.CheckRow{
		{Name: raw("name"), Type: raw("type"), Source: raw("source"), Parts: []secrets.Part{part}, Status: raw("status")},
	}}
	listed := &custody.ListResult{Context: secretResultContext(), Secrets: []custody.ListRow{
		{Name: raw("name"), Type: raw("type"), Source: raw("source"), Parts: []secrets.Part{part}, State: raw("state")},
	}}
	changed := &custody.MutationResult{Context: secretResultContext(), Name: raw("name"), Changed: 1, Parts: []secrets.Part{part}}
	if err := writeSecretCheckText(&check, checked); err != nil {
		t.Fatal(err)
	}
	if err := writeSecretListText(&list, listed); err != nil {
		t.Fatal(err)
	}
	if err := writeSecretMutation(&mutation, "secret set", changed); err != nil {
		t.Fatal(err)
	}
	checkRow := []string{shown("name"), shown("type"), shown("source"), shown("part"), shown("status"), "-"}
	listRow := []string{shown("name"), shown("type"), shown("source"), shown("part"), shown("state"), "-", "0"}
	if rows := strings.Split(strings.TrimSuffix(check.String(), "\n"), "\n"); len(rows) != 2 || !slices.Equal(strings.Fields(rows[1]), checkRow) {
		t.Fatalf("secret check text = %q, want the row %q", check.String(), checkRow)
	}
	if rows := strings.Split(strings.TrimSuffix(list.String(), "\n"), "\n"); len(rows) != 2 || !slices.Equal(strings.Fields(rows[1]), listRow) {
		t.Fatalf("secret list text = %q, want the row %q", list.String(), listRow)
	}
	if !strings.Contains(mutation.String(), "  Name       "+shown("name")+"\n") || !strings.Contains(mutation.String(), "  Parts      "+shown("part")+"\n") {
		t.Fatalf("secret mutation text = %q", mutation.String())
	}
}

func TestUninitializedEncryptionStatusHasStableEmptyShape(t *testing.T) {
	record := &dispatchRecord{result: commandResult{encryptionStatus: &encryption.StatusResult{Keys: []secretstore.Key{}}}}
	code, out, errOut := runSecretResult([]string{"secret", "encryption", "status", "--output", "json"}, record)
	wantResult := "\"result\":{\"initialized\":false,\"implementation\":null,\"activeKey\":null,\"keys\":[],\"items\":{\"currentVersions\":0,\"boundVersions\":0,\"materialParts\":0,\"retainedArtifacts\":0,\"cleanupRequired\":false}}"
	if code != 0 || errOut != "" || !strings.Contains(out, wantResult) {
		t.Fatalf("uninitialized code=%d stdout=%q stderr=%q", code, out, errOut)
	}
}

func TestSecretMetadataResultsRejectUnsupportedShapes(t *testing.T) {
	list := &custody.ListResult{Context: secretResultContext(), Secrets: []custody.ListRow{{Name: "hidden", Type: "token", Source: "file", Parts: []secrets.Part{secrets.ValuePart}, State: "current"}}}
	record := &dispatchRecord{result: commandResult{secretList: list}}
	code, out, errOut := runSecretResult([]string{"secret", "list", "--output", "json"}, record)
	if code != 1 || strings.Contains(out, "hidden") || !strings.Contains(out, "runtime.internal") || errOut != "" {
		t.Fatalf("file-backed list code=%d stdout=%q stderr=%q", code, out, errOut)
	}
	for name, row := range map[string]custody.CheckRow{
		"retired file source": {Name: "hidden", Type: "token", Source: "file", Parts: []secrets.Part{secrets.ValuePart}, Status: "available"},
		"live-read status":    {Name: "hidden", Type: "token", Source: "contextStore", Parts: []secrets.Part{secrets.ValuePart}, Status: "unreadable"},
	} {
		check := &custody.CheckResult{Context: secretResultContext(), Secrets: []custody.CheckRow{row}}
		record := &dispatchRecord{result: commandResult{secretCheck: check}}
		code, out, errOut := runSecretResult([]string{"secret", "check", "--output", "json"}, record)
		if code != 1 || strings.Contains(out, "hidden") || !strings.Contains(out, "runtime.internal") || errOut != "" {
			t.Fatalf("%s: check code=%d stdout=%q stderr=%q", name, code, out, errOut)
		}
	}
}
