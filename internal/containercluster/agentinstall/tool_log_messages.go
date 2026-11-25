package agentinstall

import (
	"bytes"
	"strings"
	"time"
)

// Project reads the installer's logrus text envelope and classifies the
// message. It does not forward the message, structured fields, hostnames,
// or credentials, including on warnings and failures.
func (l InstallerLog) Project(line []byte, since time.Time) string {
	stamp, rest, ok := strings.Cut(string(bytes.TrimSpace(line)), " level=")
	if !ok {
		return ""
	}
	stamp, ok = strings.CutPrefix(stamp, "time=")
	if !ok {
		return ""
	}
	logged, err := time.Parse(time.RFC3339Nano, strings.Trim(stamp, `"`))
	if err != nil || logged.Before(since.Truncate(time.Second)) {
		return ""
	}
	level, message, ok := strings.Cut(rest, " msg=")
	if !ok {
		return ""
	}
	message = strings.TrimPrefix(message, `"`)
	switch level {
	case "warning", "warn":
		return "Installer reported a warning; see the native log for details"
	case "error", "fatal", "panic":
		return "Installer reported an error; see the native log for details"
	case "info", "debug":
		return l.progressMessage(message)
	default:
		return ""
	}
}

func (l InstallerLog) progressMessage(message string) string {
	if l.implementation == MediaImplementation {
		switch {
		case strings.HasPrefix(message, "Downloading base ISO"):
			return "Downloading the base ISO"
		case strings.HasPrefix(message, "Extracting base ISO"):
			return "Extracting the base ISO"
		case strings.HasPrefix(message, "Creating Agent Installer ISO"):
			return "Creating the agent installer ISO"
		case strings.HasPrefix(message, "Generated ISO"):
			return "Generated the agent installer ISO"
		}
		return ""
	}
	switch {
	case strings.HasPrefix(message, "Bootstrap configMap status is complete"), strings.HasPrefix(message, "Bootstrap is complete"):
		return "OpenShift bootstrap is complete"
	case strings.HasPrefix(message, "Install complete!"):
		return "OpenShift installation is complete"
	case strings.HasPrefix(message, "Host: ") && strings.Contains(message, "reached installation stage"):
		return "An agent host reached a new installation stage"
	case strings.HasPrefix(message, "Waiting up to "):
		switch {
		case strings.Contains(message, "Kubernetes API"):
			return "Waiting for the Kubernetes API"
		case strings.Contains(message, "bootstrap"):
			return "Waiting for OpenShift bootstrap"
		case strings.Contains(message, "cluster operators"):
			return "Waiting for the cluster operators"
		case strings.Contains(message, "initialize"):
			return "Waiting for the cluster to initialize"
		}
	}
	return ""
}
