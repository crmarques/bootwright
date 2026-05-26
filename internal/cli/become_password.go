package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

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
