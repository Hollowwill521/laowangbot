// Package echo is a minimal compiled-in plugin.
package echo

import (
	"context"
	_ "embed"
	"github.com/OrionG-hub/laowangbot/pkg/pluginapi"
)

//go:embed greeting.txt
var greeting string

type Plugin struct{}

func Open(_ context.Context, _ pluginapi.Host, _ string) (pluginapi.Plugin, error) {
	return &Plugin{}, nil
}
func (*Plugin) Handle(_ context.Context, _ pluginapi.Request) pluginapi.Response {
	return pluginapi.Response{Version: pluginapi.Version, Text: greeting}
}
func (*Plugin) Close() {}
