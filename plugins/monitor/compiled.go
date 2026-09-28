package monitor

import (
	"context"
	"github.com/OrionG-hub/laowangbot/pkg/pluginapi"
	"path/filepath"
)

// Open adapts the monitor constructor to the compiled plugin API.
func Open(_ context.Context, host pluginapi.Host, stateDir string) (pluginapi.Plugin, error) {
	m, err := New(host, filepath.Join(stateDir, "monitor.json"))
	if err != nil {
		return nil, err
	}
	return &compiledMonitor{Monitor: m}, nil
}

type compiledMonitor struct{ *Monitor }

func (*compiledMonitor) Close() {}
