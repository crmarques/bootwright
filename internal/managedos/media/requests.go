package media

// AddMediaRequest imports exactly one image into the host-wide store. The
// store selects no context, so no request carries a context name.
type AddMediaRequest struct {
	Name             string
	SourceFile       string
	SourceURL        string
	SHA256           string
	SkipConfirmation bool
}

type ListMediaRequest struct {
	Checksums bool
}

type DeleteMediaRequest struct {
	Name             string
	SkipConfirmation bool
}

// MutationResult reports the exact image a mutation published or removed.
// Outcome is `stored`, `replaced` or `deleted`. A deletion carries the size
// and digest of the record it removed when that record could be read, and
// otherwise only the name.
type MutationResult struct {
	Name    string
	Size    int64
	SHA256  string
	Outcome string
}

// MediaRow is one stored image as a listing presents it. Size and SHA256 are
// its record, and empty when that record could not be read. ReservedBy names
// the contexts that reserve it, sorted, and Frozen is true exactly when one
// does. Verified is `mismatch` when the bytes held no longer have the recorded
// size or, with checksums, the recorded digest; `failed` when the store could
// not read the image's record, its file or its bytes, and Failure names why;
// `ok` only when checksums proved both size and digest, and empty otherwise.
// Computed is the digest checksums computed, and empty without them.
type MediaRow struct {
	Name       string
	Size       int64
	SHA256     string
	Source     string
	Added      string
	Frozen     bool
	ReservedBy []string
	Verified   string
	Computed   string
	Failure    string
}

// ListResult is the store's inventory in name order. Checksums reports that
// every image was read in full.
type ListResult struct {
	Media     []MediaRow
	Checksums bool
}
