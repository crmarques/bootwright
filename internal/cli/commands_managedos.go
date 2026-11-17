package cli

import (
	"context"

	"github.com/crmarques/bootwright/internal/managedos/media"
)

func mediaCommands() []commandSpec {
	return []commandSpec{
		available(commandSpec{path: "media add", validate: validateMediaAdd, classify: classifyRules{ambient: mediaAmbient, urlStdin: urlFromStdin}, example: "--name installer.iso --from-file ./installer.iso", contextFree: true, invoke: Services.invokeMediaAdd, short: "Import and verify an installer image", flags: []flagSpec{nameFlag(), fileFlag("from-file", "Import a local image"), undisclosedFlag("from-url", "Import an HTTPS image"), fileFlag("from-url-file", "Import the HTTPS image whose URL a file holds, or - for standard input"), emptyFlag("sha256", "Verify a SHA-256 digest"), confirmationFlag()}, long: "Import one installer image into this host's media store, where every context shares it. A download requires --sha256 and follows no redirect. A URL whose query carries a signature is a credential: --from-url refuses any query, so give it with --from-url-file <path> or --from-url-file -; --from-url-file - requires --yes. Replacing a stored image needs ordinary confirmation; --yes skips it."}),
		available(commandSpec{path: "media list", example: "--checksums", contextFree: true, invoke: Services.invokeMediaList, short: "List installer images", flags: []flagSpec{boolFlag("checksums", "Compute image checksums")}, long: "List the images this host stores with their sizes, recorded digests and the contexts that reserve them. The default reads records and file metadata only; --checksums reads every image in full and reports each computed digest and whether the image still matches its record."}),
		available(commandSpec{path: "media delete", validate: validateMediaName, example: "--name installer.iso", contextFree: true, invoke: Services.invokeMediaDelete, short: "Delete an unbound installer image", flags: []flagSpec{nameFlag(), confirmationFlag()}, long: "Delete one stored image and its record. An image any context reserves is refused, because a frozen operation still needs exactly those bytes."}),
	}
}

type MediaService interface {
	Add(context.Context, media.AddMediaRequest) (*media.MutationResult, error)
	List(context.Context, media.ListMediaRequest) (*media.ListResult, error)
	Delete(context.Context, media.DeleteMediaRequest) (*media.MutationResult, error)
}

// The media store is host-wide, so no request carries a context name and an
// explicit --context changes nothing.
func (s Services) invokeMediaAdd(ctx context.Context, values *requestValues, _ []string) (commandResult, error) {
	if s.Media == nil {
		return commandResult{}, errMissingService
	}
	result, err := invokeResult(ctx, values, media.AddMediaRequest{
		Name:             values.text("name"),
		SourceFile:       values.text("from-file"),
		SourceURL:        values.text("from-url"),
		SourceURLFile:    values.text("from-url-file"),
		SHA256:           values.text("sha256"),
		SkipConfirmation: values.boolean("yes"),
	}, s.Media.Add)
	return commandResult{mediaMutation: result}, err
}

func (s Services) invokeMediaList(ctx context.Context, values *requestValues, _ []string) (commandResult, error) {
	if s.Media == nil {
		return commandResult{}, errMissingService
	}
	result, err := invokeResult(ctx, values, media.ListMediaRequest{
		Checksums: values.boolean("checksums"),
	}, s.Media.List)
	return commandResult{mediaList: result}, err
}

func (s Services) invokeMediaDelete(ctx context.Context, values *requestValues, _ []string) (commandResult, error) {
	if s.Media == nil {
		return commandResult{}, errMissingService
	}
	result, err := invokeResult(ctx, values, media.DeleteMediaRequest{
		Name:             values.text("name"),
		SkipConfirmation: values.boolean("yes"),
	}, s.Media.Delete)
	return commandResult{mediaMutation: result}, err
}
