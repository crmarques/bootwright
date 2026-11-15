package lifecycle

import (
	"context"

	"github.com/crmarques/bootwright/internal/controller"
)

func (s Service) AccessTool(ctx context.Context, contextName string, tool controller.InstalledTool) (string, error) {
	if err := s.available(ctx); err != nil {
		return "", err
	}
	name, err := s.resolve(ctx, contextName)
	if err != nil {
		return "", err
	}
	var executable string
	err = s.workspace.ReadLifecycle(ctx, name, func(view View) error {
		store := s.store(view)
		index, err := store.Index(ctx)
		if err != nil {
			return err
		}
		if index.Current == "" {
			return failure("access.unavailable", "this context has no installed controller clients", "run bootwright apply --context "+name+" --stage controller")
		}
		operation, err := store.ReadOperation(ctx, index.Current)
		if err != nil {
			return err
		}
		plan, err := store.ReadPlan(ctx, operation.ID)
		if err != nil {
			return err
		}
		executable, err = s.locateTool(ctx, store, view.Controller(), name, operation, plan, tool)
		return err
	})
	return executable, err
}
