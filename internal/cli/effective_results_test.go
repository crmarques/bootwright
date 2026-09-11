package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	stateencoding "github.com/crmarques/bootwright/internal/desiredstate/encoding"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

func syntheticEffectiveResult() *compilation.EffectiveResult {
	env := api.NewObject(api.Environment, "example", api.Value{}, api.MapValue(api.FieldValue{Name: "domains", Value: api.MapValue(api.FieldValue{Name: "base", Value: api.StringValue("example.test")})}))
	vars := api.MapValue(api.FieldValue{Name: "integer", Value: api.IntegerValue("18446744073709551616000000001")}, api.FieldValue{Name: "text", Value: api.StringValue("<literal>\\u003c\n\"é")})
	playbook := api.NewObject(api.CustomPlaybook, "reserved", api.Value{}, api.MapValue(api.FieldValue{Name: "extraVars", Value: vars}))
	return &compilation.EffectiveResult{Counts: compilation.Counts{FilesSeen: 3, ObjectsDecoded: 4}, Effective: api.NewCatalog([]api.Object{playbook, env})}
}

func TestEffectiveSuccessPreservesCanonicalBytesAndNativeValues(t *testing.T) {
	result := syntheticEffectiveResult()
	canonicalYAML, err := stateencoding.YAML(context.Background(), result.Effective)
	if err != nil {
		t.Fatal(err)
	}
	canonicalJSON, err := stateencoding.JSON(context.Background(), result.Effective)
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"text", "json"} {
		var out, errOut bytes.Buffer
		record := &dispatchRecord{result: commandResult{effective: result}}
		code := New(Config{Out: &out, ErrOut: &errOut, Services: dispatchSpies(record), EncodeEffectiveYAML: stateencoding.YAML, EncodeEffectiveJSON: stateencoding.JSON}).Run(context.Background(), []string{"render", "effective", "--context", "example", "--output", mode})
		if code != 0 || errOut.Len() != 0 || record.calls != 1 || record.request.(compilation.EffectiveRequest).ContextName != "example" {
			t.Fatal(code, out.String(), errOut.String())
		}
		if mode == "text" {
			if !bytes.Equal(out.Bytes(), canonicalYAML) {
				t.Fatalf("effective YAML changed: %q", out.String())
			}
			continue
		}
		want := `{"schemaVersion":"v1alpha1","command":"render effective","ok":true,"exitCode":0,"result":{"counts":{"filesSeen":3,"objectsDecoded":4},"effectiveState":` + strings.TrimSuffix(string(canonicalJSON), "\n") + `},"diagnostics":[],"logs":[]}` + "\n"
		if out.String() != want {
			t.Fatalf("effective envelope changed:\n%s", out.String())
		}
		var decoded struct {
			Result struct{ EffectiveState []map[string]any }
		}
		decoder := json.NewDecoder(bytes.NewReader(out.Bytes()))
		decoder.UseNumber()
		if err := decoder.Decode(&decoded); err != nil {
			t.Fatal(err)
		}
		vars := decoded.Result.EffectiveState[1]["spec"].(map[string]any)["extraVars"].(map[string]any)
		if vars["integer"].(json.Number).String() != "18446744073709551616000000001" || vars["text"] != "<literal>\\u003c\n\"é" {
			t.Fatal("canonical native values changed", vars)
		}
	}
}

func TestEffectiveFailuresHaveNoPartialResult(t *testing.T) {
	for _, failure := range []error{nil, contexts.StateError("context does not exist"), errors.New("private failure detail"), context.Canceled, context.DeadlineExceeded, diagnostics.NewFailure("api.required", "required field missing", "input.yaml")} {
		for _, mode := range []string{"text", "json"} {
			record := &dispatchRecord{err: failure}
			if failure != nil {
				record.result.effective = syntheticEffectiveResult()
			}
			var out, errOut bytes.Buffer
			code := New(Config{Out: &out, ErrOut: &errOut, Services: dispatchSpies(record), EncodeEffectiveYAML: stateencoding.YAML, EncodeEffectiveJSON: stateencoding.JSON}).Run(context.Background(), []string{"render", "effective", "--output", mode})
			if code != 1 || strings.Contains(out.String()+errOut.String(), "private failure detail") {
				t.Fatal(code, out.String(), errOut.String())
			}
			if mode == "text" {
				if out.Len() != 0 || errOut.Len() == 0 {
					t.Fatal("partial effective output", out.String(), errOut.String())
				}
			} else {
				var envelope struct {
					OK          bool
					Result      any
					Diagnostics []diagnostic
				}
				if err := json.Unmarshal(out.Bytes(), &envelope); err != nil || envelope.OK || envelope.Result != nil || len(envelope.Diagnostics) != 1 || errOut.Len() != 0 {
					t.Fatal("failed effective envelope", out.String(), errOut.String(), err)
				}
			}
		}
	}
}

func TestEffectiveEncodingFailurePrecedesOutput(t *testing.T) {
	for _, mode := range []string{"text", "json"} {
		broken := api.NewCatalog([]api.Object{api.NewObject(api.CustomPlaybook, "reserved", api.Value{}, api.MapValue(api.FieldValue{Name: "extraVars", Value: api.MapValue(api.FieldValue{Name: "number", Value: api.NumberValue(".nan")})}))})
		record := &dispatchRecord{result: commandResult{effective: &compilation.EffectiveResult{Effective: broken}}}
		var out, errOut bytes.Buffer
		code := New(Config{Out: &out, ErrOut: &errOut, Services: dispatchSpies(record), EncodeEffectiveYAML: stateencoding.YAML, EncodeEffectiveJSON: stateencoding.JSON}).Run(context.Background(), []string{"render", "effective", "--output", mode})
		if code != 1 || !strings.Contains(out.String()+errOut.String(), "runtime.encode") || strings.Contains(out.String()+errOut.String(), "absent value") {
			t.Fatal(code, out.String(), errOut.String())
		}
		if mode == "text" && out.Len() != 0 {
			t.Fatal("partial YAML", out.String())
		}
		if mode == "json" && (!strings.Contains(out.String(), `"result":null`) || strings.Count(out.String(), "\n") != 1 || errOut.Len() != 0) {
			t.Fatal("partial JSON", out.String(), errOut.String())
		}
	}
}

func TestEffectiveWriterFailuresAndCancellation(t *testing.T) {
	for _, mode := range []string{"text", "json"} {
		for _, writer := range []io.Writer{rejectingWriter{}, rejectingWriter{short: true}, contextErrorWriter{}} {
			record := &dispatchRecord{result: commandResult{effective: syntheticEffectiveResult()}}
			var errOut bytes.Buffer
			code := New(Config{Out: writer, ErrOut: &errOut, Services: dispatchSpies(record), EncodeEffectiveYAML: stateencoding.YAML, EncodeEffectiveJSON: stateencoding.JSON}).Run(context.Background(), []string{"render", "effective", "--output", mode})
			if code != 1 || errOut.Len() != 0 {
				t.Fatal("effective writer fallback", code, errOut.String())
			}
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		var out bytes.Buffer
		err := writeEffective(ctx, &out, "render effective", syntheticEffectiveResult(), mode == "json", stateencoding.JSON)
		var failure *resultFailure
		if !errors.As(err, &failure) || failure.code != "runtime.canceled" || out.Len() != 0 {
			t.Fatal("canceled effective encoding", err, out.String())
		}
	}
}

func TestEffectiveEncoderCapabilitiesAreLazyAndModeSpecific(t *testing.T) {
	for _, mode := range []string{"text", "json"} {
		calls := 0
		encode := func(ctx context.Context, catalog api.Catalog) ([]byte, error) {
			calls++
			if mode == "json" {
				return stateencoding.JSON(ctx, catalog)
			}
			return stateencoding.YAML(ctx, catalog)
		}
		other := func(context.Context, api.Catalog) ([]byte, error) {
			t.Fatal("wrong encoder capability")
			return nil, nil
		}
		record := &dispatchRecord{result: commandResult{effective: syntheticEffectiveResult()}}
		config := Config{Services: dispatchSpies(record), EncodeEffectiveYAML: encode, EncodeEffectiveJSON: other}
		if mode == "json" {
			config.EncodeEffectiveYAML, config.EncodeEffectiveJSON = other, encode
		}
		runner := New(config)
		if calls != 0 {
			t.Fatal("construction encoded state")
		}
		for _, args := range [][]string{{"render", "effective", "--help"}, {"render", "effective", "--unknown"}, {"version"}, {"__bootwright_complete", "render", "effective", ""}} {
			runner.Run(context.Background(), args)
		}
		if calls != 0 || record.calls != 0 {
			t.Fatal("informational path consumed encoder or service")
		}
		if code := runner.Run(context.Background(), []string{"render", "effective", "--output", mode}); code != 0 || calls != 1 {
			t.Fatal("wrong encoding count", code, calls)
		}
	}
}

func TestMissingAndUntrustworthyEffectiveEncodersFailBeforeOutput(t *testing.T) {
	for _, encode := range []func(context.Context, api.Catalog) ([]byte, error){
		nil,
		func(context.Context, api.Catalog) ([]byte, error) {
			return []byte("partial"), errors.New("private encoder detail")
		},
		func(context.Context, api.Catalog) ([]byte, error) { return []byte("invalid JSON"), nil },
	} {
		var out, errOut bytes.Buffer
		record := &dispatchRecord{result: commandResult{effective: syntheticEffectiveResult()}}
		code := New(Config{Out: &out, ErrOut: &errOut, Services: dispatchSpies(record), EncodeEffectiveJSON: encode}).Run(context.Background(), []string{"render", "effective", "--output", "json"})
		if code != 1 || !strings.Contains(out.String(), `"result":null`) || !strings.Contains(out.String(), `"code":"runtime.encode"`) || strings.Contains(out.String(), "private encoder detail") || strings.Contains(out.String(), "partial") || errOut.Len() != 0 {
			t.Fatal(code, out.String(), errOut.String())
		}
	}
}
