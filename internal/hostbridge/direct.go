package hostbridge

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/OrionG-hub/laowangbot/internal/bot"
	"github.com/OrionG-hub/laowangbot/pkg/pluginapi"
	"time"
)

type directHost struct {
	client  func() *bot.Client
	allowed map[string]bool
}

// Direct provides the capability-scoped host API without opening a listener.
func Direct(client func() *bot.Client, capabilities []string) (pluginapi.Host, error) {
	h := &directHost{client: client, allowed: map[string]bool{}}
	for _, c := range capabilities {
		if !methods[c] {
			return nil, errors.New("unknown host capability: " + c)
		}
		h.allowed[c] = true
	}
	return h, nil
}
func (h *directHost) Call(ctx context.Context, q pluginapi.Call) (pluginapi.Result, error) {
	if !h.allowed[q.Method] {
		return pluginapi.Result{}, errors.New("capability denied")
	}
	data, err := json.Marshal(q)
	if err != nil {
		return pluginapi.Result{}, err
	}
	if len(data) > 1<<20 {
		return pluginapi.Result{}, errors.New("host request too large")
	}
	if err := ctx.Err(); err != nil {
		return pluginapi.Result{}, err
	}
	if h.client == nil {
		return pluginapi.Result{}, errDisconnected
	}
	b := h.client()
	if b == nil || b.Self() == nil {
		return pluginapi.Result{}, errDisconnected
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	result, err := dispatch(ctx, b, q)
	if err != nil {
		return result, err
	}
	if len(result.Bytes) > MaxMedia {
		return pluginapi.Result{}, errors.New("media exceeds 8 MiB")
	}
	data, err = json.Marshal(result)
	if err != nil {
		return pluginapi.Result{}, err
	}
	if len(data) > 12<<20 {
		return pluginapi.Result{}, errors.New("response too large")
	}
	return result, nil
}
