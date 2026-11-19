package inspection

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/crmarques/bootwright/internal/secrets"
)

var ErrMonitoringMaterial = errors.New("the custodied monitoring material is not one login of its service")

const GrafanaUser = "admin"

func (s Service) revealMonitoring(ctx context.Context, selected string, entry StorageEntry) error {
	for i := range entry.Ceph.Monitoring {
		access := &entry.Ceph.Monitoring[i]
		if access.Service == "grafana" {
			custodied, found, err := s.custody.GrafanaPassword(ctx, selected, entry.Name)
			if err != nil {
				custodied.Material.Clear()
				return err
			}
			if !found {
				access.Anonymous = true
				continue
			}
			value, _ := custodied.Material.Part(secrets.ValuePart)
			empty := len(value) == 0
			clear(value)
			if empty || !slices.Equal(custodied.Material.Parts(), []secrets.Part{secrets.ValuePart}) {
				custodied.Material.Clear()
				return failure("secret.store.corrupt", "the Secret bound as Grafana's admin password of "+entry.Kind+" "+entry.Name+" holds no value", "restore the original context custody before continuing")
			}
			access.User, access.Password, access.Revealed = GrafanaUser, custodied.Material, true
			continue
		}
		custodied, found, err := s.custody.MonitoringCredentials(ctx, selected, entry.Name, access.Service)
		if err == nil && found && !validObjectCredentials(custodied.Material) {
			err = ErrMonitoringMaterial
		}
		if err != nil {
			custodied.Material.Clear()
			if errors.Is(err, ErrMonitoringMaterial) {
				return failure("secret.store.corrupt", "this context holds an invalid "+serviceTitle(access.Service)+" login for "+entry.Kind+" "+entry.Name, "restore the original context custody before continuing")
			}
			return err
		}
		if !found {
			return failure("access.unavailable",
				"this context holds no "+serviceTitle(access.Service)+" login for "+entry.Kind+" "+entry.Name+"; an apply keeps it once its management gateway is ready",
				"bootwright apply --context "+selected+" --stage clusters")
		}
		user, _ := custodied.Material.Part(secrets.UsernamePart)
		password, _ := custodied.Material.Part(secrets.PasswordPart)
		custodied.Material.Clear()
		access.User, access.Password, access.Revealed, access.Warn = string(user), secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: password}), true, custodied.Unproved
		clear(user)
		clear(password)
	}
	return nil
}

func serviceTitle(service string) string {
	return strings.ToUpper(service[:1]) + service[1:]
}
