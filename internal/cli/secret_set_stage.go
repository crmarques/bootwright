package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/crmarques/bootwright/internal/runtime/fs"
	"github.com/crmarques/bootwright/internal/runtime/secrets"
)

func runSecretSetWithLocalRoot(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer, name, pullSecret, tlsCert, tlsKey, rawFile, fromFile, username, password string, passwordStdin, generate bool, yes bool) (int, error) {
	rootArgs, rootStdin, cleanup, err := stagedSecretSetRootArgs(stdin, name, pullSecret, tlsCert, tlsKey, rawFile, fromFile, username, password, passwordStdin, generate, yes)
	if err != nil {
		return 1, err
	}
	defer cleanup()
	return runWithLocalRoot(ctx, rootArgs, rootStdin, stdout, stderr, false)
}

func stagedSecretSetRootArgs(stdin io.Reader, name, pullSecret, tlsCert, tlsKey, rawFile, fromFile, username, password string, passwordStdin, generate bool, yes bool) ([]string, io.Reader, func(), error) {
	rootArgs := []string{"secret", "set", name}
	if yes {
		rootArgs = append(rootArgs, "--yes")
	}
	rootStdin := stdin
	tempDir := ""
	cleanup := func() {
		if tempDir != "" {
			_ = os.RemoveAll(tempDir)
		}
	}
	stage := func(filename string, data []byte) (string, error) {
		if tempDir == "" {
			dir, err := os.MkdirTemp("", "bootwright-secret-set-")
			if err != nil {
				return "", fmt.Errorf("create temporary secret input: %w", err)
			}
			if err := os.Chmod(dir, 0o700); err != nil {
				_ = os.RemoveAll(dir)
				return "", fmt.Errorf("chmod temporary secret input %s: %w", dir, err)
			}
			tempDir = dir
		}
		path := filepath.Join(tempDir, filename)
		if err := safefs.WriteNewFile(path, data, 0o600); err != nil {
			return "", err
		}
		return path, nil
	}

	switch {
	case pullSecret != "":
		data, err := os.ReadFile(pullSecret)
		if err != nil {
			cleanup()
			return nil, nil, func() {}, fmt.Errorf("read pull secret file %s: %w", pullSecret, err)
		}
		if err := secret.ValidatePullSecretJSON(data); err != nil {
			cleanup()
			return nil, nil, func() {}, err
		}
		path, err := stage("pull-secret.json", data)
		if err != nil {
			cleanup()
			return nil, nil, func() {}, err
		}
		rootArgs = append(rootArgs, "--pull-secret", path)
	case tlsCert != "":
		certData, err := os.ReadFile(tlsCert)
		if err != nil {
			cleanup()
			return nil, nil, func() {}, fmt.Errorf("read TLS certificate file %s: %w", tlsCert, err)
		}
		keyData, err := os.ReadFile(tlsKey)
		if err != nil {
			cleanup()
			return nil, nil, func() {}, fmt.Errorf("read TLS private key file %s: %w", tlsKey, err)
		}
		if _, err := secret.ValidateTLSCertificateKey(certData, keyData); err != nil {
			cleanup()
			return nil, nil, func() {}, err
		}
		certPath, err := stage("tls.crt", certData)
		if err != nil {
			cleanup()
			return nil, nil, func() {}, err
		}
		keyPath, err := stage("tls.key", keyData)
		if err != nil {
			cleanup()
			return nil, nil, func() {}, err
		}
		rootArgs = append(rootArgs, "--tls-cert", certPath, "--tls-key", keyPath)
	case rawFile != "":
		data, err := os.ReadFile(rawFile)
		if err != nil {
			cleanup()
			return nil, nil, func() {}, fmt.Errorf("read raw secret file %s: %w", rawFile, err)
		}
		path, err := stage("raw-secret", data)
		if err != nil {
			cleanup()
			return nil, nil, func() {}, err
		}
		rootArgs = append(rootArgs, "--raw-file", path)
	case fromFile != "":
		data, err := os.ReadFile(fromFile)
		if err != nil {
			cleanup()
			return nil, nil, func() {}, fmt.Errorf("read credentials file %s: %w", fromFile, err)
		}
		if _, _, err := secret.ParseBMCCredentials(data); err != nil {
			cleanup()
			return nil, nil, func() {}, err
		}
		path, err := stage("credentials", data)
		if err != nil {
			cleanup()
			return nil, nil, func() {}, err
		}
		rootArgs = append(rootArgs, "--from-file", path)
	case password != "":
		path, err := stageCredentialsInput(stage, username, password)
		if err != nil {
			cleanup()
			return nil, nil, func() {}, err
		}
		rootArgs = append(rootArgs, "--from-file", path)
	case passwordStdin:
		if stdin == nil {
			cleanup()
			return nil, nil, func() {}, errors.New("--password-stdin requires stdin")
		}
		line, err := bufio.NewReader(stdin).ReadString('\n')
		if err != nil && line == "" {
			cleanup()
			return nil, nil, func() {}, fmt.Errorf("read password from stdin: %w", err)
		}
		path, err := stageCredentialsInput(stage, username, strings.TrimRight(line, "\r\n"))
		if err != nil {
			cleanup()
			return nil, nil, func() {}, err
		}
		rootArgs = append(rootArgs, "--from-file", path)
		rootStdin = strings.NewReader("")
	case generate:
		rootArgs = appendSecretSetUsername(rootArgs, username)
		rootArgs = append(rootArgs, "--generate")
	}
	return rootArgs, rootStdin, cleanup, nil
}

func stageCredentialsInput(stage func(string, []byte) (string, error), username, password string) (string, error) {
	if err := secret.ValidateBMCUsername(username); err != nil {
		return "", err
	}
	if password == "" {
		return "", errors.New("password must not be empty")
	}
	return stage("credentials", []byte(username+":"+password+"\n"))
}

func appendSecretSetUsername(args []string, username string) []string {
	if username == "" {
		return args
	}
	return append(args, "--username", username)
}
