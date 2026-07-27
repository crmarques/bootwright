package gitcontent

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/crmarques/bootwright/internal/host/shellquote"
)

const askpassScript = `#!/bin/sh
case "$1" in
  *[Uu]sername*) exec cat "$BOOTWRIGHT_GIT_USERNAME_FILE" ;;
  *) exec cat "$BOOTWRIGHT_GIT_PASSWORD_FILE" ;;
esac
`

func credentialEnv(req Request, scratchDir string) (func(), []string, error) {
	noop := func() {}
	cred := req.Credential
	if cred == nil {
		return noop, nil, nil
	}
	if cred.PrivateKeyPath != "" {
		command := fmt.Sprintf(
			"ssh -i %s -o IdentitiesOnly=yes -o BatchMode=yes -o StrictHostKeyChecking=accept-new",
			shellquote.QuoteWord(cred.PrivateKeyPath))
		return noop, []string{"GIT_SSH_COMMAND=" + command}, nil
	}
	if cred.Password == "" {
		return noop, nil, nil
	}
	dir, err := os.MkdirTemp(scratchDir, "git-cred-")
	if err != nil {
		return noop, nil, err
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	userFile := filepath.Join(dir, "username")
	passFile := filepath.Join(dir, "password")
	askpass := filepath.Join(dir, "askpass")
	if err := os.WriteFile(userFile, []byte(cred.Username), 0o600); err != nil {
		cleanup()
		return noop, nil, err
	}
	if err := os.WriteFile(passFile, []byte(cred.Password), 0o600); err != nil {
		cleanup()
		return noop, nil, err
	}
	if err := os.WriteFile(askpass, []byte(askpassScript), 0o700); err != nil {
		cleanup()
		return noop, nil, err
	}
	return cleanup, []string{
		"GIT_ASKPASS=" + askpass,
		"BOOTWRIGHT_GIT_USERNAME_FILE=" + userFile,
		"BOOTWRIGHT_GIT_PASSWORD_FILE=" + passFile,
	}, nil
}
