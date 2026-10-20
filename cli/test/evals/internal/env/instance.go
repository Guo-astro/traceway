package env

import (
	"context"
	"fmt"
	"path/filepath"
)

// Instance is a booted, registered backend with its monitor stub.
type Instance struct {
	Backend *Backend
	Admin   *Admin
	Stub    *Stub
	Dir     string
}

// AdminEmail and AdminPassword are the owner credentials of every instance.
const (
	AdminEmail    = "owner@example.com"
	AdminPassword = "eval-owner-password"
)

// NewInstance boots a backend under dir/backend, registers the owner and
// organization, and starts a healthy stub.
func NewInstance(ctx context.Context, bins *Binaries, dir string) (*Instance, error) {
	b, err := Boot(ctx, bins.Backend, BootOptions{Dir: filepath.Join(dir, "backend")})
	if err != nil {
		return nil, err
	}
	a, err := Register(ctx, b, AdminEmail, AdminPassword, "Acme")
	if err != nil {
		b.Stop()
		return nil, fmt.Errorf("register: %w", err)
	}
	stub, err := StartStub(true)
	if err != nil {
		b.Stop()
		return nil, err
	}
	return &Instance{Backend: b, Admin: a, Stub: stub, Dir: dir}, nil
}

// Close stops everything.
func (i *Instance) Close() {
	if i.Stub != nil {
		i.Stub.Close()
	}
	if i.Backend != nil {
		i.Backend.Stop()
	}
}
