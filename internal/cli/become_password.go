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
	if in == nil {
		return "", errors.New("cannot read BECOME password without stdin")
	}
	if prompt == nil {
		prompt = io.Discard
	}
	fmt.Fprint(prompt, "BECOME password: ")
	if file, ok := in.(*os.File); ok {
		password, usedTerminal, err := readPasswordNoEcho(file)
		if usedTerminal {
			fmt.Fprintln(prompt)
			return password, err
		}
	}
	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && line == "" {
		return "", fmt.Errorf("read BECOME password: %w", err)
	}
	return strings.TrimRight(line, "\r\n"), nil
}
