package cli

import (
	"bufio"
	"os"
	"strings"

	"golang.org/x/sys/unix"
)

func readPasswordNoEcho(file *os.File) (string, bool, error) {
	fd := int(file.Fd())
	termios, err := unix.IoctlGetTermios(fd, unix.TCGETS)
	if err != nil {
		return "", false, nil
	}
	withoutEcho := *termios
	withoutEcho.Lflag &^= unix.ECHO
	if err := unix.IoctlSetTermios(fd, unix.TCSETS, &withoutEcho); err != nil {
		return "", false, nil
	}
	defer unix.IoctlSetTermios(fd, unix.TCSETS, termios)
	line, err := bufio.NewReader(file).ReadString('\n')
	if err != nil && line == "" {
		return "", true, err
	}
	return strings.TrimRight(line, "\r\n"), true, nil
}
