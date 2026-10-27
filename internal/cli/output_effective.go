package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
)

// effectiveResult carries the canonical effective-state array exactly as its
// encoder wrote it.
type effectiveResult struct {
	Counts         admissionCounts   `json:"counts"`
	EffectiveState json.RawMessage   `json:"effectiveState"`
	Origins        []effectiveOrigin `json:"origins,omitempty"`
}

type effectiveOrigin struct {
	Object string `json:"object"`
	Field  string `json:"field"`
	Origin string `json:"origin"`
}

func effectiveOrigins(result *compilation.EffectiveResult) []effectiveOrigin {
	var origins []effectiveOrigin
	for _, object := range result.Origins {
		for _, field := range object.Fields {
			origins = append(origins, effectiveOrigin{Object: escapeDisplayLine(object.Object), Field: escapeDisplayLine(field.Field), Origin: escapeDisplayLine(field.Origin)})
		}
	}
	return origins
}

func writeOriginListing(out io.Writer, result *compilation.EffectiveResult) error {
	var listing bytes.Buffer
	for _, object := range result.Origins {
		listing.WriteString(escapeDisplayLine(object.Object) + "\n")
		for _, field := range object.Fields {
			listing.WriteString("  " + escapeDisplayLine(field.Field) + ": " + escapeDisplayLine(field.Origin) + "\n")
		}
	}
	_, err := out.Write(listing.Bytes())
	return err
}

func (effectiveResult) documentedResult() {}

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
	if !jsonMode && result.Origins != nil {
		return writeOriginListing(out, result)
	}
	if jsonMode {
		payload := effectiveResult{Counts: displayCounts(result.Counts), EffectiveState: json.RawMessage(data), Origins: effectiveOrigins(result)}
		var encoded bytes.Buffer
		if err := writeEnvelope(WithPresentation(&encoded, presentationFor(out)), commandEnvelope{SchemaVersion: "v1alpha1", Command: escapeDisplayLine(command), OK: true, Result: payload, Diagnostics: []diagnostic{}, Logs: []string{}}); err != nil {
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
