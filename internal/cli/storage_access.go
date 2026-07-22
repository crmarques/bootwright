package cli

import (
	cliout "github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/clusteraccess"
)

func printStorageAccessSections(p *cliout.Printer, summaries []clusteraccess.StorageSummary, showSecrets bool) {
	for _, summary := range summaries {
		p.Section(summary.Name + ":")
		p.Fields(storageAccessLeadFields(summary))
		if len(summary.MonitorEndpoints) > 0 {
			p.FieldList("Monitors", summary.MonitorEndpoints)
		}
		p.Fields(storageAccessTrailFields(summary, showSecrets))
	}
}

func storageAccessLeadFields(summary clusteraccess.StorageSummary) []cliout.Field {
	fields := []cliout.Field{{Key: "Type", Value: storageAccessTypeDetail(summary)}}
	if summary.SeedHost != "" {
		fields = append(fields, cliout.Field{Key: "Seed node", Value: summary.SeedHost})
	}
	if summary.SSHCommand != "" {
		fields = append(fields, cliout.Field{Key: "SSH", Value: summary.SSHCommand})
	}
	return fields
}

func storageAccessTrailFields(summary clusteraccess.StorageSummary, showSecrets bool) []cliout.Field {
	var fields []cliout.Field
	if summary.HealthCommand != "" {
		fields = append(fields, cliout.Field{Key: "Health check", Value: summary.HealthCommand})
	}
	if summary.ShellCommand != "" {
		fields = append(fields, cliout.Field{Key: "Cluster shell", Value: summary.ShellCommand})
	}
	if summary.DashboardURL != "" {
		fields = append(fields, cliout.Field{Key: "Dashboard", Value: summary.DashboardURL})
	}
	if summary.DashboardPasswordPath != "" {
		fields = append(fields, cliout.Field{Key: "Dashboard user", Value: summary.DashboardUser})
		if showSecrets {
			fields = append(fields, cliout.Field{Key: "Dashboard password", Value: revealValue(summary.DashboardPasswordPath, summary.DashboardPassword)})
		} else {
			fields = append(fields, cliout.Field{Key: "Dashboard password file", Value: summary.DashboardPasswordPath})
		}
	}
	return append(fields,
		cliout.Field{Key: "ceph.conf", Value: storageAccessNodePath(summary.ConfigPath, summary.SeedHost)},
		cliout.Field{Key: "Admin keyring", Value: storageAccessNodePath(summary.KeyringPath, summary.SeedHost)},
	)
}

func storageAccessTypeDetail(summary clusteraccess.StorageSummary) string {
	if summary.Management == "" {
		return summary.Type
	}
	return summary.Type + " (" + summary.Management + ")"
}

func storageAccessNodePath(path, node string) string {
	if node == "" {
		return path
	}
	return path + " (on " + node + ")"
}
