//go:build !linux

package safefs

func UnmountAllUnder(_ string) error { return nil }
