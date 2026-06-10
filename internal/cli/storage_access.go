package cli

import (
	"strings"

	cliout "github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/clusteraccess"
)

// printStorageAccessSections renders one block per storage cluster into an
// existing printer, so it composes with both the standalone cluster access command
// and the post-apply continuation.
func printStorageAccessSections(p *cliout.Printer, summaries []clusteraccess.StorageSummary) {
	for _, summary := range summaries {
		p.Section("Storage cluster " + summary.Name)
		p.Fields(storageAccessFields(summary))
		if summary.DashboardPasswordPath != "" {
			p.Status(accessArtifactStatus(summary.DashboardPassword), "dashboard password", accessArtifactDetail(summary.DashboardPassword))
		}
		p.Status(cliout.StatusInfo, "health", "run the health check to confirm Ceph reports HEALTH_OK")
	}
}

func storageAccessFields(summary clusteraccess.StorageSummary) []cliout.Field {
	fields := []cliout.Field{{Key: "Type", Value: storageAccessTypeDetail(summary)}}
	if summary.SeedHost != "" {
		fields = append(fields, cliout.Field{Key: "Seed node", Value: summary.SeedHost})
	}
	if summary.SSHCommand != "" {
		fields = append(fields, cliout.Field{Key: "SSH", Value: summary.SSHCommand})
	}
	if len(summary.MonitorEndpoints) > 0 {
		fields = append(fields, cliout.Field{Key: "Monitors", Value: strings.Join(summary.MonitorEndpoints, ", ")})
	}
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
		fields = append(fields,
			cliout.Field{Key: "Dashboard user", Value: summary.DashboardUser},
			cliout.Field{Key: "Dashboard password file", Value: summary.DashboardPasswordPath},
			cliout.Field{Key: "Show dashboard password", Value: summary.DashboardPasswordCommand},
		)
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
