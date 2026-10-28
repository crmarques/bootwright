//go:build linux && amd64

package hostlinux

import (
	"context"

	"github.com/crmarques/bootwright/internal/trust"
)

var sshHostKeyFiles = []string{"/etc/ssh/ssh_host_ed25519_key.pub", "/etc/ssh/ssh_host_ecdsa_key.pub", "/etc/ssh/ssh_host_rsa_key.pub"}

func (i Inspector) SSHHostKeys(ctx context.Context) ([]trust.HostKey, error) {
	fs, err := i.view.open(ctx)
	if err != nil {
		return nil, ctx.Err()
	}
	defer fs.close()
	var keys []trust.HostKey
	for _, name := range sshHostKeyFiles {
		data, err := fs.read(ctx, name, 16<<10)
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if err != nil {
			continue
		}
		if key, err := trust.ParseAuthorizedKey(string(data)); err == nil {
			keys = append(keys, key)
		}
	}
	return keys, nil
}
