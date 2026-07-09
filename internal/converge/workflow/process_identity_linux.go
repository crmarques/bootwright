//go:build linux

package workflow

import (
	"os"
	"strconv"
	"strings"
)

func processStartToken(pid int) (string, bool) {
	if pid <= 0 {
		return "", false
	}
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return "", false
	}
	stat := string(data)
	end := strings.LastIndexByte(stat, ')')
	if end < 0 || end+2 >= len(stat) {
		return "", false
	}
	fields := strings.Fields(stat[end+2:])
	const starttimeIndex = 19
	if len(fields) <= starttimeIndex {
		return "", false
	}
	return fields[starttimeIndex], true
}
