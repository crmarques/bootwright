package nativeexec

import (
	"context"
	"io"
	"strings"
	"testing"
)

func TestInputPipeDeliversInputAndCancelsOnRelease(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	stream, release, err := PipeInput(strings.NewReader("fixture input"), cancel)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	data, err := io.ReadAll(stream)
	release()
	if err != nil || string(data) != "fixture input" || ctx.Err() == nil {
		t.Fatal("the pipe failed to deliver input or cancel its producer")
	}
}
