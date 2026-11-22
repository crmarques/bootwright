package nativeexec

import (
	"io"
	"os"
)

func PipeInput(input io.Reader, cancel func()) (io.Reader, func(), error) {
	reader, writer, err := os.Pipe()
	if err != nil {
		return nil, nil, err
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer writer.Close()
		_, _ = io.Copy(writer, input)
	}()
	return reader, func() {
		cancel()
		_ = reader.Close()
		_ = writer.Close()
		<-done
	}, nil
}
