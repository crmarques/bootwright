package main

import (
	"context"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/managedos"
	"github.com/crmarques/bootwright/internal/managedos/installation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets/custody"
)

type secretReadiness interface {
	Readiness(context.Context, custody.ReadinessRequest) ([]custody.ReadinessRow, error)
}

func statusReadiness(secrets secretReadiness, media installation.MediaRecords) lifecycle.Readiness {
	return func(ctx context.Context, contextName string, catalog api.Catalog) ([]lifecycle.ReadinessGap, error) {
		gaps := []lifecycle.ReadinessGap{}
		var secretErr, mediaErr error
		if secrets != nil {
			var rows []custody.ReadinessRow
			if rows, secretErr = secrets.Readiness(ctx, custody.ReadinessRequest{ContextName: contextName}); secretErr == nil {
				for _, row := range rows {
					gaps = append(gaps, lifecycle.ReadinessGap{Kind: "secret", Name: row.Name, State: row.Status, Remedy: row.Remedy})
				}
			}
		}
		if names := managedos.MediaReferences(catalog); media != nil && len(names) != 0 {
			var held map[string]installation.MediaRecord
			if held, mediaErr = media.MediaRecords(ctx); mediaErr == nil {
				for _, name := range names {
					if _, ok := held[name]; !ok {
						gaps = append(gaps, lifecycle.ReadinessGap{Kind: "media", Name: name, State: "missing", Remedy: "bootwright media add --name " + name + " --from-file <path>"})
					}
				}
			}
		}
		if len(gaps) == 0 {
			if secretErr != nil {
				return nil, secretErr
			}
			return nil, mediaErr
		}
		return gaps, nil
	}
}
