package cli

import "strings"

type runSelection struct {
	stage    string
	through  string
	clusters string
	machines string
}

func (s runSelection) flags() string {
	var parts []string
	if trimmed := strings.TrimSpace(s.stage); trimmed != "" {
		parts = append(parts, "--stage "+trimmed)
	}
	if trimmed := strings.TrimSpace(s.through); trimmed != "" {
		parts = append(parts, "--through "+trimmed)
	}
	switch {
	case strings.TrimSpace(s.machines) != "":
		parts = append(parts, "--machines "+strings.TrimSpace(s.machines))
	case strings.TrimSpace(s.clusters) != "":
		parts = append(parts, "--clusters "+strings.TrimSpace(s.clusters))
	}
	return strings.Join(parts, " ")
}

func (s runSelection) command(verb string, trailing ...string) string {
	parts := []string{"bootwright", verb}
	parts = append(parts, trailing...)
	if flags := s.flags(); flags != "" {
		parts = append(parts, flags)
	}
	return strings.Join(parts, " ")
}

func (s runSelection) narrowFlag() string {
	if strings.TrimSpace(s.machines) != "" {
		return "--machines"
	}
	return "--clusters"
}
