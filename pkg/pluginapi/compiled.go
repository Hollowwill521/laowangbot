package pluginapi

import "context"

// Plugin is a statically linked, trusted Go plugin. Handle must honor context
// cancellation; the host serializes calls and never abandons a running call.
type Plugin interface {
	Handle(context.Context, Request) Response
	Close()
}

// Factory receives a lifetime context and the deployment's persistent state directory.
type Factory func(context.Context, Host, string) (Plugin, error)
