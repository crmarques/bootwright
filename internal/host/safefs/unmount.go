package safefs

import (
	"bufio"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

func mountpointsUnder(r io.Reader, dir string) ([]string, error) {
	root := filepath.Clean(dir)
	var mounts []string
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 5 {
			continue
		}
		target := unescapeMountPath(fields[4])
		if target == root || strings.HasPrefix(target, root+"/") {
			mounts = append(mounts, target)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read mount table: %w", err)
	}
	for i, j := 0, len(mounts)-1; i < j; i, j = i+1, j-1 {
		mounts[i], mounts[j] = mounts[j], mounts[i]
	}
	sort.SliceStable(mounts, func(i, j int) bool { return mounts[i] > mounts[j] })
	return mounts, nil
}

func unescapeMountPath(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) {
			if v, err := strconv.ParseUint(s[i+1:i+4], 8, 8); err == nil {
				b.WriteByte(byte(v))
				i += 3
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
