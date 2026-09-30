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

// EventFilter returns an immutable, nonblocking predicate for admission.
// The host obtains a fresh snapshot after each serialized plugin call.
type EventFilter interface{ EventFilter() func(Event) bool }
