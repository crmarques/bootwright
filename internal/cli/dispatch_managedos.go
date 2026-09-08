package cli

import (
	"context"
	"errors"

	"github.com/crmarques/bootwright/internal/managedos"
)

func (s Services) invokeMedia(ctx context.Context, path string, values *requestValues, args []string) error {
	if s.Media == nil {
		return errMissingService
	}
	switch path {
	case "media add":
		return invokeRequest(ctx, values, managedos.AddMediaRequest{
			ContextName:      values.text("context"),
			Name:             values.text("name"),
			SourceFile:       values.text("from-file"),
			SourceURL:        values.text("from-url"),
			SHA256:           values.text("sha256"),
			SkipConfirmation: values.boolean("yes"),
		}, s.Media.Add)
	case "media list":
		return invokeRequest(ctx, values, managedos.ListMediaRequest{
			ContextName: values.text("context"),
			Checksums:   values.boolean("checksums"),
		}, s.Media.List)
	case "media delete":
		return invokeRequest(ctx, values, managedos.DeleteMediaRequest{
			ContextName:      values.text("context"),
			Name:             values.text("name"),
			SkipConfirmation: values.boolean("yes"),
		}, s.Media.Delete)
	default:
		return errors.New("command has no application dispatch")
	}
}
