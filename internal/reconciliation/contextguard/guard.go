// Package contextguard owns the local lifecycle evidence used during a locked
// Workspace mutation. It grants no authority to execute a native operation.
package contextguard

import (
	"bytes"
	"context"
	"encoding/json"
	"io"

	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

type Guard struct{}

func (Guard) Check(ctx context.Context, data []byte) (contexts.Disposition, error) {
	if err := ctx.Err(); err != nil {
		return contexts.Disposition{}, err
	}
	invalid := func() (contexts.Disposition, error) {
		return contexts.Disposition{}, contexts.StateError("context mutation evidence is missing, corrupt or unsupported")
	}
	if len(data) == 0 || len(data) > 65536 {
		return invalid()
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return invalid()
	}
	seen := map[string]bool{}
	operation, ownership := "", ""
	for decoder.More() {
		token, err = decoder.Token()
		if err != nil {
			return invalid()
		}
		key, ok := token.(string)
		if !ok || seen[key] {
			return invalid()
		}
		seen[key] = true
		token, err = decoder.Token()
		if err != nil {
			return invalid()
		}
		switch key {
		case "version":
			number, ok := token.(json.Number)
			if !ok || string(number) != "1" {
				return invalid()
			}
		case "operation":
			operation, ok = token.(string)
			if !ok {
				return invalid()
			}
		case "ownership":
			ownership, ok = token.(string)
			if !ok {
				return invalid()
			}
		default:
			return invalid()
		}
	}
	token, err = decoder.Token()
	if err != nil || token != json.Delim('}') || len(seen) != 3 {
		return invalid()
	}
	if _, err = decoder.Token(); err != io.EOF {
		return invalid()
	}
	if ownership != "none" && ownership != "retained" {
		return invalid()
	}
	switch operation {
	case "none", "pending", "failed", "unknown", "applied":
	default:
		return invalid()
	}
	if err = ctx.Err(); err != nil {
		return contexts.Disposition{}, err
	}
	disposable := operation == "none" && ownership == "none"
	return contexts.Disposition{Update: operation == "none" || operation == "applied", Dispose: disposable}, nil
}
