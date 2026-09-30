package media

// NewMemoryStore is the store double storecontract.Verify holds, holding no
// image.
func NewMemoryStore() Store { return &fakeStore{} }
