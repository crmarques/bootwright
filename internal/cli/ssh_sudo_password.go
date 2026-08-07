package cli

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	cliout "github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/host/become"
	"github.com/crmarques/bootwright/internal/host/execution"
)

const sshSudoPasswordPrompt = "SSH sudo password: "

const sshSudoPasswordReuseNotice = "answering sudo as %s with the password you just entered"

var callerAccountName = execution.CallerUserName

func resolveSSHSudoPassword(in io.Reader, prompt io.Writer, sshUser string) (string, error) {
	if !sshAskSudoPassword {
		return "", nil
	}
	reused, ok, err := reuseCallerSudoPassword(prompt, sshUser)
	if err != nil {
		return "", err
	}
	if ok {
		return reused, nil
	}
	password, err := readPromptedPassword(in, prompt, sshSudoPasswordPromptFor(sshUser), true)
	if err != nil {
		return "", errors.New("--ssh-ask-sudo-password needs a terminal to prompt on: run bootwright interactively, or grant the account passwordless sudo on the machines you administer and drop the flag")
	}
	if strings.TrimSpace(password) == "" {
		return "", errors.New("--ssh-ask-sudo-password was answered with an empty password; sudo would refuse it on every machine, so nothing was started")
	}
	return password, nil
}

func reuseCallerSudoPassword(prompt io.Writer, sshUser string) (string, bool, error) {
	path := become.InheritedPasswordFile()
	if path == "" || sshUser == "" || sshUser != callerAccountName() {
		return "", false, nil
	}
	password, err := become.ReadPasswordFile(path)
	if err != nil {
		return "", false, fmt.Errorf("--ssh-ask-sudo-password names %s, the account bootwright already prompted you for, but that answer could not be read back: %w", strconv.Quote(sshUser), err)
	}
	detail := fmt.Sprintf(sshSudoPasswordReuseNotice, strconv.Quote(sshUser))
	cliout.NewContinuation(prompt).Status(cliout.StatusInfo, flagSSHAskSudoPassword, detail)
	return password, true, nil
}

func sshSudoPasswordPromptFor(sshUser string) string {
	if sshUser == "" {
		return sshSudoPasswordPrompt
	}
	return "SSH sudo password for " + strconv.Quote(sshUser) + ": "
}
