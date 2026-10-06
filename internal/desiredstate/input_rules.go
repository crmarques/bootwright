package desiredstate

import (
	"slices"
	"strconv"
	"strings"
)

var skippedDirectoryNames = []string{"vendor", "node_modules", "playbooks", "roles", "collections", "manifests", "secrets"}

func LimitMessage(resource string, ceiling int) string {
	return resource + " exceeds its inclusive ceiling of " + strconv.Itoa(ceiling)
}

func SkippedDirectory(name string) bool {
	return strings.HasPrefix(name, ".") || slices.Contains(skippedDirectoryNames, name)
}
