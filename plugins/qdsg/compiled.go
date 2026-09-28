package qdsg

import (
	"context"
	_ "embed"
	"github.com/OrionG-hub/laowangbot/pkg/pluginapi"
)

// Open adapts the existing constructor to the compiled plugin API.
func Open(ctx context.Context, host pluginapi.Host, stateDir string) (pluginapi.Plugin, error) {
	return New(ctx, host, stateDir)
}

//go:embed manifest.json
var manifestJSON string

// ManifestJSON returns the metadata shipped with the bundled implementation.
func ManifestJSON() string { return manifestJSON }
