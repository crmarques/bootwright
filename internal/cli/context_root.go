package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/crmarques/bootwright/internal/contextstore"
)

func shouldRunContextRootChild() bool {
	return shouldRunLocalRootChild()
}

func runContextImportWithLocalRoot(ctx context.Context, baseArgs []string, files []string, yes bool, stdin io.Reader, stdout io.Writer, stderr io.Writer) (int, error) {
	inputDir, cleanup, err := stageContextInputForRoot(files)
	if err != nil {
		return 1, err
	}
	defer cleanup()
	rootArgs := append(append([]string(nil), baseArgs...), "-f", inputDir)
	if yes {
		rootArgs = append(rootArgs, "--yes")
	}
	return runWithLocalRoot(ctx, rootArgs, stdin, stdout, stderr, true)
}

func stageContextInputForRoot(files []string) (string, func(), error) {
	tempDir, err := os.MkdirTemp("", "bootwright-context-input-")
	if err != nil {
		return "", nil, fmt.Errorf("create temporary context input: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(tempDir) }
	inputDir := filepath.Join(tempDir, contextstore.InputDirName)
	if _, err := contextstore.ImportInputs(files, inputDir, false); err != nil {
		cleanup()
		return "", nil, err
	}
	return inputDir, cleanup, nil
}
