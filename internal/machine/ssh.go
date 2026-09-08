package machine

type SSHOptions struct {
	IdentityFile       string
	User               string
	AskSudoPassword    bool
	UserForProvisioned bool
}
