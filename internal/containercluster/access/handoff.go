package access

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/crmarques/bootwright/internal/availability"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/machine"
)

type Client struct {
	Executable   string
	Version      string
	Distribution string
}

type Handoff struct {
	Context           string
	Cluster           string
	Client            Client
	Arguments         []string
	KubeconfigCommand []string
}

func (s Service) WithMetadata(metadata AccessMetadata) Service {
	s.metadata = metadata
	return s
}

func (s Service) handoff(ctx context.Context, executable, contextName, cluster string, args []string) (*Handoff, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.state == nil || s.metadata == nil {
		return nil, availability.ErrNotImplemented
	}
	selected, err := machine.SelectedContext(ctx, s.selection, contextName)
	if err != nil {
		return nil, err
	}
	effective, err := s.state.RenderEffective(ctx, compilation.EffectiveRequest{ContextName: selected})
	if err != nil {
		return nil, err
	}
	if err := accessTarget(effective.Effective, selected, cluster, executable); err != nil {
		return nil, err
	}
	if !safeArguments(args) {
		return nil, failure("access.handoff", "the client arguments cannot be represented safely in an access descriptor", "pass a non-empty bounded argument vector without credential, target, plugin or configuration overrides; use cluster kubeconfig for independent access")
	}
	client, err := s.metadata.Client(ctx, selected, cluster, executable)
	if err != nil {
		return nil, err
	}
	if !filepath.IsAbs(client.Executable) || !safeText(client.Executable) || client.Version == "" || !safeText(client.Version) || !safeText(client.Distribution) {
		return nil, failure("access.handoff", "the installed client identity cannot be represented safely", "run bootwright apply --context "+selected+" --stage controller")
	}
	return &Handoff{Context: selected, Cluster: cluster, Client: client,
		Arguments: slices.Clone(args), KubeconfigCommand: []string{"bootwright", "cluster", "kubeconfig", "--context", selected, "--name", cluster}}, nil
}

func safeText(value string) bool {
	return len(value) <= 4096 && utf8.ValidString(value) && !strings.ContainsAny(value, "\x00\r\n")
}

func safeArguments(args []string) bool {
	if len(args) == 0 || len(args) > 256 || args[0] == "" || strings.HasPrefix(args[0], "-") {
		return false
	}
	if slices.Contains([]string{"login", "logout", "config", "plugin"}, args[0]) {
		return false
	}
	total := 0
	for _, arg := range args {
		total += len(arg)
		if !safeText(arg) || total > 32<<10 || strings.HasPrefix(arg, "-s") && !strings.HasPrefix(arg, "--") {
			return false
		}
		flag, _, _ := strings.Cut(arg, "=")
		if args[0] == "whoami" && strings.HasPrefix(flag, "-") && !strings.HasPrefix(flag, "--") && strings.Contains(flag, "t") {
			return false
		}
		switch flag {
		case "--from-literal", "--password-stdin", "--show-token", "--kubeconfig", "--context", "--cluster", "--user", "--server", "-s", "--token", "--username", "--password", "--client-key", "--client-certificate", "--certificate-authority", "--insecure-skip-tls-verify", "--tls-server-name", "--proxy-url", "--cache-dir":
			return false
		}
	}
	return true
}

func UnavailableCluster(contextName, cluster string) error {
	return failure("access.unavailable", "this context holds no completed installation and administrator kubeconfig for OpenShiftCluster "+cluster, "bootwright apply --context "+contextName)
}
