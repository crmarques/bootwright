package inspection

import (
	"context"
	"slices"

	"github.com/crmarques/bootwright/internal/secrets"
)

func (s Service) WithConsoleCustody(custody ConsoleCustody) Service {
	s.consoleCustody = custody
	return s
}

func (s Service) revealConsole(ctx context.Context, selected string, entry *ContainerEntry) error {
	if s.consoleCustody == nil {
		return failure("access.unavailable", "console password custody is not configured", "use the installation directory's auth/kubeadmin-password")
	}
	held, found, err := s.consoleCustody.ConsolePassword(ctx, selected, entry.Name)
	if err != nil {
		held.Material.Clear()
		return err
	}
	if !found || held.Unproved {
		held.Material.Clear()
		return failure("access.unavailable", "this context holds no proved console administrator password for "+entry.Kind+" "+entry.Name,
			"use the original installation directory's auth/kubeadmin-password or run bootwright cluster update-admin-creds --context "+selected+" --name "+entry.Name+" --login console")
	}
	password, ok := held.Material.Part(secrets.ValuePart)
	defer clear(password)
	valid := ok && slices.Equal(held.Material.Parts(), []secrets.Part{secrets.ValuePart}) && len(password) >= 23 && len(password) <= 128
	for _, b := range password {
		valid = valid && b >= 0x21 && b <= 0x7e
	}
	if !valid {
		held.Material.Clear()
		return failure("secret.store.corrupt", "the console administrator password in custody is invalid", "restore this context's original credential custody")
	}
	if entry.Details == nil {
		entry.Details = &ContainerDetails{}
	}
	entry.Details.Console = &ConsoleAccess{User: "kubeadmin", Password: held.Material, Revealed: true}
	return nil
}
