package main

import (
	"github.com/crmarques/bootwright/internal/cli"
	"github.com/crmarques/bootwright/internal/machine/power"
	"github.com/crmarques/bootwright/internal/secrets/custody"
)

// The process's confirmer names the context of every per-object prompt, so
// each consumer that asks about one object of a context reaches that prompt
// rather than the one that names the context alone.
var (
	_ custody.ContextConfirmer = (*cli.Confirmation)(nil)
	_ power.ContextConfirmer   = (*cli.Confirmation)(nil)
)
