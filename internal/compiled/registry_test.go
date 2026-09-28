package compiled

import (
	"context"
	"github.com/OrionG-hub/laowangbot/internal/plugin"
	"github.com/OrionG-hub/laowangbot/pkg/pluginapi"
	"testing"
)

func TestRegistryOwnsManifestSlices(t *testing.T) {
	original := Entries()
	defer func() { mu.Lock(); entries = original; mu.Unlock() }()
	m := plugin.Manifest{Name: "demo", Commands: []string{"hello"}, Events: []string{"message"}, Capabilities: []string{"self"}, Args: []string{"legacy"}}
	Register(m, func(context.Context, pluginapi.Host, string) (pluginapi.Plugin, error) { return nil, nil })
	m.Commands[0] = "mutated"
	first := Entries()
	e := first[len(first)-1]
	if e.Manifest.Commands[0] != "hello" || e.New == nil {
		t.Fatal("registry retained mutable input")
	}
	e.Manifest.Commands[0] = "changed"
	e.Manifest.Events[0] = "changed"
	e.Manifest.Capabilities[0] = "changed"
	e.Manifest.Args[0] = "changed"
	second := Entries()
	e = second[len(second)-1]
	if e.Manifest.Commands[0] != "hello" || e.Manifest.Events[0] != "message" || e.Manifest.Capabilities[0] != "self" || e.Manifest.Args[0] != "legacy" {
		t.Fatal("registry exposed internal slices")
	}
}
