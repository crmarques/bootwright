package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
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
	if path := inheritedBecomePasswordFile(); path != "" {
		credential := becomeCredential{PasswordFile: path}
		if needPassword {
			password, err := readBecomePasswordFile(path)
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
		path, fileCleanup, err := writeBecomePasswordFile(password)
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

func inheritedBecomePasswordFile() string {
	if strings.TrimSpace(os.Getenv(localRootSudoAuthEnv)) != localSudoAuthPrompted {
		return ""
	}
	return strings.TrimSpace(os.Getenv(localRootBecomePasswordFileEnv))
}

func willPromptForBecomePassword(ask bool) bool {
	return ask && inheritedBecomePasswordFile() == ""
}

func becomePasswordSummary(unit string) string {
	if inheritedBecomePasswordFile() != "" {
		return "Bootwright will reuse the sudo password already validated for this command"
	}
	return "Bootwright will ask once for the BECOME password and reuse it for this " + unit
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
		if inheritedBecomePasswordFile() != "" {
			return "ready (prompted once)"
		}
		return "ready"
	default:
		return ""
	}
}

func readBecomePasswordFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read inherited BECOME password file: %w", err)
	}
	password := strings.TrimRight(string(data), "\r\n")
	if password == "" {
		return "", errors.New("inherited BECOME password file is empty")
	}
	return password, nil
}

func prepareBecomePasswordFile(in io.Reader, prompt io.Writer) (string, func(), error) {
	password, err := readBecomePassword(in, prompt)
	if err != nil {
		return "", nil, err
	}
	return writeBecomePasswordFile(password)
}

func writeBecomePasswordFile(password string) (string, func(), error) {
	if password == "" {
		return "", nil, errors.New("BECOME password cannot be empty")
	}
	file, err := os.CreateTemp("", "bootwright-become-*")
	if err != nil {
		return "", nil, fmt.Errorf("create BECOME password file: %w", err)
	}
	path := file.Name()
	cleanup := func() { _ = os.Remove(path) }
	if err := os.Chmod(path, 0o600); err != nil {
		_ = file.Close()
		cleanup()
		return "", nil, fmt.Errorf("chmod BECOME password file: %w", err)
	}
	if _, err := file.WriteString(password + "\n"); err != nil {
		_ = file.Close()
		cleanup()
		return "", nil, fmt.Errorf("write BECOME password file: %w", err)
	}
	if err := file.Close(); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("close BECOME password file: %w", err)
	}
	return path, cleanup, nil
}

func readBecomePassword(in io.Reader, prompt io.Writer) (string, error) {
	return readPromptedPassword(in, prompt, "BECOME password: ", false)
}

func readSudoPassword(in io.Reader, prompt io.Writer) (string, error) {
	return readPromptedPassword(in, prompt, "SUDO password: ", true)
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
