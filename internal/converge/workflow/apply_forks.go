package workflow

import (
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/render"
)

func AnsibleForksForLimit(state v1alpha1.State, limit string) int {
	members := render.HostGroupMembers(state)
	selected := map[string]bool{}
	addAll := func(hosts []string) {
		for _, host := range hosts {
			if host != "" {
				selected[host] = true
			}
		}
	}
	if strings.TrimSpace(limit) == "" {
		for _, hosts := range members {
			addAll(hosts)
		}
	} else {
		for _, token := range strings.Split(limit, ":") {
			token = strings.TrimSpace(token)
			if token == "" {
				continue
			}
			if hosts, ok := members[token]; ok {
				addAll(hosts)
				continue
			}
			selected[token] = true
		}
	}
	if len(selected) < 1 {
		return 1
	}
	return len(selected)
}
