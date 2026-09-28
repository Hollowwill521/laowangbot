package qdsg

import (
	"context"
	"github.com/OrionG-hub/laowangbot/pkg/pluginapi"
)

// Open adapts the existing constructor to the compiled plugin API.
func Open(ctx context.Context, host pluginapi.Host, stateDir string) (pluginapi.Plugin, error) {
	return New(ctx, host, stateDir)
}
