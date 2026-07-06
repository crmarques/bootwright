package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/crmarques/bootwright/internal/host/become"
)

const (
	localRootSudoAuthEnv           = become.SudoAuthEnv
	localRootBecomePasswordFileEnv = become.PasswordFileEnv
)

const (
	localSudoAuthNonInteractive = become.AuthNonInteractive
	localSudoAuthPrompted       = become.AuthPrompted
)

type becomeCredential struct {
	Password     string
	PasswordFile string
	Prompted     bool
}

func prepareBecomeCredential(in io.Reader, prompt io.Writer, ask, needPassword, needPasswordFile bool) (becomeCredential, func(), error) {
	if !ask || (!needPassword && !needPasswordFile) {
		return becomeCredential{}, func() {}, nil
	}
	if path := become.InheritedPasswordFile(); path != "" {
		credential := becomeCredential{PasswordFile: path}
		if needPassword {
			password, err := become.ReadPasswordFile(path)
			if err != nil {
				return becomeCredential{}, nil, err
			}
			credential.Password = password
		}
		if !needPasswordFile {
			credential.PasswordFile = ""
		}
		return credential, func() {}, nil
	}
	password, err := readBecomePassword(in, prompt)
	if err != nil {
		return becomeCredential{}, nil, err
	}
	if password == "" {
		return becomeCredential{}, nil, errors.New("BECOME password cannot be empty")
	}
	credential := becomeCredential{Password: password, Prompted: true}
	cleanup := func() {}
	if needPasswordFile {
		path, fileCleanup, err := become.WritePasswordFile(password)
		if err != nil {
			return becomeCredential{}, nil, err
		}
		credential.PasswordFile = path
		cleanup = fileCleanup
	}
	if !needPassword {
		credential.Password = ""
	}
	return credential, cleanup, nil
}

func willPromptForBecomePassword(ask bool) bool {
	return ask && become.InheritedPasswordFile() == ""
}

var askBecomePassDefault = func() bool {
	switch os.Getenv(localRootSudoAuthEnv) {
	case localSudoAuthPrompted:
		return true
	case localSudoAuthNonInteractive:
		return false
	default:
		return os.Geteuid() != 0
	}
}

func localRootSudoSummary() string {
	switch os.Getenv(localRootSudoAuthEnv) {
	case localSudoAuthNonInteractive:
		return "ready (non-interactive)"
	case localSudoAuthPrompted:
		if become.InheritedPasswordFile() != "" {
			return "ready (prompted once)"
		}
		return "ready"
	default:
		return ""
	}
}

func prepareBecomePasswordFile(in io.Reader, prompt io.Writer) (string, func(), error) {
	password, err := readBecomePassword(in, prompt)
	if err != nil {
		return "", nil, err
	}
	return become.WritePasswordFile(password)
}

func readBecomePassword(in io.Reader, prompt io.Writer) (string, error) {
	return readPromptedPassword(in, prompt, "BECOME password: ", false)
}

func readSudoPassword(in io.Reader, prompt io.Writer) (string, error) {
	password, err := readPromptedPassword(in, prompt, "SUDO password: ", true)
	if err != nil {
		// A bare "read password: EOF" gives no hint why a read-only command wants
		// a password. bootwright re-executes itself under sudo to reach
		// /var/lib/bootwright, so name that and the ways to satisfy it.
		return "", fmt.Errorf("bootwright needs root to access /var/lib/bootwright: run interactively so it can prompt for the sudo password, as root, or with passwordless sudo (%w)", err)
	}
	return password, nil
}

var openControllingTTY = func() (*os.File, error) {
	return os.OpenFile("/dev/tty", os.O_RDWR, 0)
}

func readPromptedPassword(in io.Reader, prompt io.Writer, promptText string, fallbackTTY bool) (string, error) {
	if in == nil && !fallbackTTY {
		return "", errors.New("cannot read password without stdin")
	}
	if prompt == nil {
		prompt = io.Discard
	}
	fmt.Fprint(prompt, promptText)
	if file, ok := in.(*os.File); ok {
		password, usedTerminal, err := readPasswordNoEcho(file)
		if usedTerminal {
			fmt.Fprintln(prompt)
			return password, err
		}
	}
	if fallbackTTY {
		if tty, err := openControllingTTY(); err == nil {
			defer tty.Close()
			password, usedTerminal, err := readPasswordNoEcho(tty)
			if usedTerminal {
				fmt.Fprintln(prompt)
				return password, err
			}
		}
	}
	if in == nil {
		return "", errors.New("cannot read password without stdin")
	}
	line, err := readPasswordLine(in)
	if err != nil && line == "" {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

func readPasswordLine(in io.Reader) (string, error) {
	if br, ok := in.(*bufio.Reader); ok {
		line, err := br.ReadString('\n')
		if err != nil && line == "" {
			return "", fmt.Errorf("read password: %w", err)
		}
		return line, nil
	}
	var b strings.Builder
	buf := make([]byte, 1)
	for {
		n, err := in.Read(buf)
		if n > 0 {
			b.WriteByte(buf[0])
			if buf[0] == '\n' {
				return b.String(), nil
			}
		}
		if err != nil {
			if err == io.EOF && b.Len() > 0 {
				return b.String(), nil
			}
			return "", fmt.Errorf("read password: %w", err)
		}
	}
}
