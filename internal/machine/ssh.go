package machine

type SSHOptions struct {
	IdentityFile       string
	User               string
	AskSudoPassword    bool
	UserForProvisioned bool
}

// Borrowed reports whether the invocation supplied an operator credential of
// its own. A lifecycle operation uses only the Machine's authored access.
func (o SSHOptions) Borrowed() bool {
	return o.IdentityFile != "" || o.User != "" || o.AskSudoPassword || o.UserForProvisioned
}
