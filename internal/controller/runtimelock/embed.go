package runtimelock

import _ "embed"

//go:embed lock.json
var embedded []byte

func Embedded() (Lock, error) { return Parse(embedded) }
