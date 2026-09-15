package cli

import (
	"context"
	"errors"

	"github.com/crmarques/bootwright/internal/managedos/media"
)

func mediaCommands() []commandSpec {
	return []commandSpec{
		available(commandSpec{path: "media add", short: "Import and verify an installer image", flags: []flagSpec{nameFlag(), fileFlag("from-file", "Import a local image"), stringFlag("from-url", "Import an HTTP or HTTPS image"), stringFlag("sha256", "Verify a SHA-256 digest"), confirmationFlag()}, long: "Import one installer image into this host's media store, where every context shares it. A download requires --sha256 and follows no redirect. Replacing a stored image needs ordinary confirmation; --yes skips it."}),
		available(commandSpec{path: "media list", short: "List installer images", flags: []flagSpec{boolFlag("checksums", "Compute image checksums"), outputFlag()}, long: "List the images this host stores. The default reads records and file metadata only; --checksums reads every image in full and reports whether its bytes still match its record."}),
		available(commandSpec{path: "media delete", short: "Delete an unbound installer image", flags: []flagSpec{nameFlag(), confirmationFlag()}, long: "Delete one stored image and its record. An image any context reserves is refused, because a frozen operation still needs exactly those bytes."}),
	}
}

type MediaService interface {
	Add(context.Context, media.AddMediaRequest) (*media.MutationResult, error)
	List(context.Context, media.ListMediaRequest) (*media.ListResult, error)
	Delete(context.Context, media.DeleteMediaRequest) (*media.MutationResult, error)
}

// invokeMedia translates one media request. The store is host-wide, so no
// request carries a context name and an explicit --context changes nothing.
func (s Services) invokeMedia(ctx context.Context, path string, values *requestValues, args []string) (commandResult, error) {
	if s.Media == nil {
		return commandResult{}, errMissingService
	}
	var result commandResult
	var err error
	switch path {
	case "media add":
		result.mediaMutation, err = invokeResult(ctx, values, media.AddMediaRequest{
			Name:             values.text("name"),
			SourceFile:       values.text("from-file"),
			SourceURL:        values.text("from-url"),
			SHA256:           values.text("sha256"),
			SkipConfirmation: values.boolean("yes"),
		}, s.Media.Add)
	case "media list":
		result.mediaList, err = invokeResult(ctx, values, media.ListMediaRequest{
			Checksums: values.boolean("checksums"),
		}, s.Media.List)
	case "media delete":
		result.mediaMutation, err = invokeResult(ctx, values, media.DeleteMediaRequest{
			Name:             values.text("name"),
			SkipConfirmation: values.boolean("yes"),
		}, s.Media.Delete)
	default:
		return commandResult{}, errors.New("command has no application dispatch")
	}
	return result, err
}
