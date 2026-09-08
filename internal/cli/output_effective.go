package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
)

func writeEffective(ctx context.Context, out io.Writer, command string, result *compilation.EffectiveResult, jsonMode bool, encode func(context.Context, api.Catalog) ([]byte, error)) error {
	if err := ctx.Err(); err != nil {
		return encodingFailure(ctx, err)
	}
	if encode == nil {
		return &resultFailure{"runtime.encode", "effective state encoder is not configured", 1}
	}
	data, err := encode(ctx, result.Effective)
	if canceled := ctx.Err(); canceled != nil {
		return encodingFailure(ctx, canceled)
	}
	if err != nil {
		return encodingFailure(ctx, err)
	}
	if len(data) == 0 || data[len(data)-1] != '\n' || jsonMode && (len(data) < 3 || data[0] != '[' || data[len(data)-2] != ']') {
		return &resultFailure{"runtime.encode", "effective state encoder returned an invalid representation", 1}
	}
	if jsonMode {
		payload := struct {
			Counts         compilation.Counts `json:"counts"`
			EffectiveState json.RawMessage    `json:"effectiveState"`
		}{Counts: result.Counts, EffectiveState: json.RawMessage(data)}
		var encoded bytes.Buffer
		encoder := json.NewEncoder(&encoded)
		encoder.SetEscapeHTML(false)
		if err := encoder.Encode(commandEnvelope{SchemaVersion: "v1alpha1", Command: command, OK: true, Result: payload, Diagnostics: []diagnostic{}, Logs: []string{}}); err != nil {
			return encodingFailure(ctx, err)
		}
		data = encoded.Bytes()
	}
	if err := ctx.Err(); err != nil {
		return encodingFailure(ctx, err)
	}
	_, err = out.Write(data)
	return err
}
