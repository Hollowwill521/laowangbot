package extensions

import (
	"context"
	"github.com/OrionG-hub/laowangbot/internal/plugin"
)

// sourceManager runs each mutation in the builder's private staging deployment.
type sourceManager struct {
	plugin.Manager
	ctx     context.Context
	apply   func(context.Context, func(plugin.Manager) error) error
	changed bool
}

func (m *sourceManager) change(ctx context.Context, f func(plugin.Manager) error) error {
	err := m.apply(ctx, f)
	if err == nil {
		m.changed = true
	}
	return err
}
func (m *sourceManager) InstallLocal(p string) error {
	return m.change(m.ctx, func(stage plugin.Manager) error { return stage.InstallLocal(p) })
}
func (m *sourceManager) ReplaceLocal(p string) error {
	return m.change(m.ctx, func(stage plugin.Manager) error { return stage.ReplaceLocal(p) })
}
func (m *sourceManager) Remove(n string) error {
	return m.change(m.ctx, func(stage plugin.Manager) error { return stage.Remove(n) })
}
func (m *sourceManager) InstallRemote(ctx context.Context, n string) error {
	return m.change(ctx, func(stage plugin.Manager) error { return stage.InstallRemote(ctx, n) })
}
func (m *sourceManager) UpdateRemoteForce(ctx context.Context, n string, force bool) error {
	return m.change(ctx, func(stage plugin.Manager) error { return stage.UpdateRemoteForce(ctx, n, force) })
}
func (m *sourceManager) ImportPackage(data []byte, replace bool) (string, error) {
	var name string
	err := m.change(m.ctx, func(stage plugin.Manager) error { var e error; name, e = stage.ImportPackage(data, replace); return e })
	return name, err
}

func (m *sourceManager) Installed() ([]plugin.InstalledInfo, error) {
	items, err := m.Manager.Installed()
	for i := range items {
		if items[i].Manifest.ProtocolVersion != 2 && items[i].Error == "" {
			items[i].Error = "旧独立进程插件已停用；请用 Go 源码包 replace 迁移，或 rm 卸载"
		}
	}
	return items, err
}
