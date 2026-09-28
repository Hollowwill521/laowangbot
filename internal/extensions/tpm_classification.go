package extensions

import (
	"context"
	"fmt"
	"github.com/OrionG-hub/laowangbot/internal/command"
	"github.com/OrionG-hub/laowangbot/internal/plugin"
)

type managedTPM struct {
	tpmManager
	registry *command.Registry
}

func (m managedTPM) UpdateRemoteForce(ctx context.Context, name string, force bool) error {
	items, e := m.Installed()
	if e != nil {
		return e
	}
	for _, item := range items {
		if item.Manifest.Name == name {
			if item.Source != plugin.CatalogURL {
				return fmt.Errorf("%s 是手动插件，请使用 tpm replace 更新", name)
			}
			return m.tpmManager.UpdateRemoteForce(ctx, name, force)
		}
	}
	if _, ok := m.registry.Lookup(name); ok {
		return fmt.Errorf("%s 是内置命令，请使用 .update check / .update run 更新主程序", name)
	}
	return fmt.Errorf("插件 %s 未安装；可使用 tpm search 查询", name)
}

func (m managedTPM) Batch(ctx context.Context, apply func(tpmManager) (bool, error)) error {
	if batch, ok := m.tpmManager.(tpmBatcher); ok {
		return batch.Batch(ctx, func(stage tpmManager) (bool, error) {
			return apply(managedTPM{stage, m.registry})
		})
	}
	_, err := apply(m)
	return err
}
