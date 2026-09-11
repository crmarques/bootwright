package cli

import (
	"context"
	"errors"

	"github.com/crmarques/bootwright/internal/managedos/media"
)

func mediaCommands() []commandSpec {
	return []commandSpec{
		{path: "media add", short: "Import and verify an installer image", flags: []flagSpec{nameFlag(), fileFlag("from-file", "Import a local image"), stringFlag("from-url", "Import an HTTP or HTTPS image"), stringFlag("sha256", "Verify a SHA-256 digest"), confirmationFlag()}},
		{path: "media list", short: "List installer images", flags: []flagSpec{boolFlag("checksums", "Compute image checksums"), outputFlag()}},
		{path: "media delete", short: "Delete an unbound installer image", flags: []flagSpec{nameFlag(), confirmationFlag()}},
	}
}

type MediaService interface {
	Add(context.Context, media.AddMediaRequest) error
	List(context.Context, media.ListMediaRequest) error
	Delete(context.Context, media.DeleteMediaRequest) error
}

func (s Services) invokeMedia(ctx context.Context, path string, values *requestValues, args []string) error {
	if s.Media == nil {
		return errMissingService
	}
	switch path {
	case "media add":
		return invokeRequest(ctx, values, media.AddMediaRequest{
			ContextName:      values.text("context"),
			Name:             values.text("name"),
			SourceFile:       values.text("from-file"),
			SourceURL:        values.text("from-url"),
			SHA256:           values.text("sha256"),
			SkipConfirmation: values.boolean("yes"),
		}, s.Media.Add)
	case "media list":
		return invokeRequest(ctx, values, media.ListMediaRequest{
			ContextName: values.text("context"),
			Checksums:   values.boolean("checksums"),
		}, s.Media.List)
	case "media delete":
		return invokeRequest(ctx, values, media.DeleteMediaRequest{
			ContextName:      values.text("context"),
			Name:             values.text("name"),
			SkipConfirmation: values.boolean("yes"),
		}, s.Media.Delete)
	default:
		return errors.New("command has no application dispatch")
	}
}
