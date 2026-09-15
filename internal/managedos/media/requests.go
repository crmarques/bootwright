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
// Outcome is `stored`, `replaced` or `deleted`.
type MutationResult struct {
	Name    string
	Size    int64
	SHA256  string
	Outcome string
}

// MediaRow is one stored image as a listing presents it. Verified is empty
// unless checksums were requested, and otherwise `ok` or `mismatch`.
type MediaRow struct {
	Name     string
	Size     int64
	SHA256   string
	Source   string
	Added    string
	Frozen   bool
	Verified string
}

type ListResult struct {
	Media []MediaRow
}
