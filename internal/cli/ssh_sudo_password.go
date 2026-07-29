package cli

import (
	"errors"
	"io"
	"strings"
)

const sshSudoPasswordPrompt = "SSH sudo password: "

func resolveSSHSudoPassword(in io.Reader, prompt io.Writer) (string, error) {
	if !sshAskSudoPassword {
		return "", nil
	}
	password, err := readPromptedPassword(in, prompt, sshSudoPasswordPrompt, true)
	if err != nil {
		return "", errors.New("--ssh-ask-sudo-password needs a terminal to prompt on: run bootwright interactively, or grant the account passwordless sudo on the machines you administer and drop the flag")
	}
	if strings.TrimSpace(password) == "" {
		return "", errors.New("--ssh-ask-sudo-password was answered with an empty password; sudo would refuse it on every machine, so nothing was started")
	}
	return password, nil
}
