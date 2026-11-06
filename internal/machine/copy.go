package machine

import (
	"errors"
	"strings"
	"unicode/utf8"
)

type CopyPath struct {
	Remote bool
	Path   string
}

func ParseCopyPaths(operands []string) (CopyPath, CopyPath, error) {
	if len(operands) != 2 {
		return CopyPath{}, CopyPath{}, errors.New("machine cp requires exactly two operands: local:<path> remote:<path>, or the reverse")
	}
	paths := make([]CopyPath, 2)
	for i, operand := range operands {
		prefix, name, ok := strings.Cut(operand, ":")
		if !ok || (prefix != "local" && prefix != "remote") || name == "" || strings.ContainsRune(name, 0) || !utf8.ValidString(name) || len(name) > 4096 {
			return CopyPath{}, CopyPath{}, errors.New("each copy operand must be local:<path> or remote:<path> with a non-empty UTF-8 path of at most 4096 bytes")
		}
		paths[i] = CopyPath{Remote: prefix == "remote", Path: name}
	}
	if paths[0].Remote == paths[1].Remote {
		return CopyPath{}, CopyPath{}, errors.New("machine cp requires one local operand and one remote operand")
	}
	return paths[0], paths[1], nil
}
