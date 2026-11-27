package main

import (
	"context"
	"io"
	"path/filepath"
	"slices"

	"github.com/crmarques/bootwright/internal/infrastructureservices/artifactcatalog"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

type artifactStore interface {
	AddArtifact(context.Context, artifactcatalog.Entry, io.Reader) (artifactcatalog.Entry, bool, error)
	ListArtifacts(context.Context) ([]artifactcatalog.Row, error)
	DeleteArtifact(context.Context, artifactcatalog.Entry) error
	ResolveArtifact(context.Context, string) (artifactcatalog.Entry, string, error)
}

type artifactCommands struct {
	store     artifactStore
	files     operatorFiles
	confirmer contexts.Confirmer
}

func (c artifactCommands) Add(ctx context.Context, entry artifactcatalog.Entry, source string) (artifactcatalog.Entry, bool, error) {
	if source == "" {
		return c.store.AddArtifact(ctx, entry, nil)
	}
	if c.files == nil {
		return entry, false, artifactcatalog.Failure("the invoking account's file reader is unavailable", "run the command as the operator on the controller")
	}
	absolute, err := filepath.Abs(source)
	if err != nil {
		return entry, false, artifactcatalog.Failure("the local artifact path could not be resolved", "supply the path to a readable regular file")
	}
	session, err := c.files.Begin(ctx)
	if err != nil {
		return entry, false, err
	}
	defer session.Close()
	file, err := session.OpenFile(absolute)
	if err != nil {
		return entry, false, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return entry, false, artifactcatalog.Failure("the local artifact source is not a readable regular file", "supply the path to a readable regular file")
	}
	return c.store.AddArtifact(ctx, entry, file)
}

func (c artifactCommands) List(ctx context.Context, name string) ([]artifactcatalog.Row, error) {
	rows, err := c.store.ListArtifacts(ctx)
	if err != nil || name == "" {
		return rows, err
	}
	if index := slices.IndexFunc(rows, func(row artifactcatalog.Row) bool { return row.Name == name }); index >= 0 {
		return rows[index : index+1], nil
	}
	return nil, artifactcatalog.Failure("catalog artifact "+name+" is missing", artifactcatalog.Supply(name))
}

func (c artifactCommands) Delete(ctx context.Context, name string, yes bool) (artifactcatalog.Entry, error) {
	rows, err := c.List(ctx, name)
	if err != nil {
		return artifactcatalog.Entry{}, err
	}
	if len(rows) != 1 || rows[0].Name != name {
		return artifactcatalog.Entry{}, artifactcatalog.Failure("the artifact name is missing", "supply the exact catalog entry name")
	}
	entry := rows[0].Entry
	if !yes {
		if c.confirmer == nil {
			return entry, artifactcatalog.Failure("artifact deletion requires confirmation", "repeat with --yes to confirm deletion")
		}
		if err := c.confirmer.Confirm(ctx, "delete catalog artifact", name+" (sha256:"+entry.SHA256+")"); err != nil {
			return entry, err
		}
	}
	return entry, c.store.DeleteArtifact(ctx, entry)
}
