// Package rename lets the user customize the brand name shown in
// user-visible output such as .status and .sysinfo:
//
//	.rename <名字>   set a custom brand name (persisted, survives restarts)
//	.rename reset   restore the default name
//	.rename         show the current brand name
package rename

import (
	"context"
	"encoding/json"
	"html"
	"strings"

	api "github.com/OrionG-hub/laowangbot/pkg/pluginapi"
)

// Plugin implements api.Plugin.
type Plugin struct {
	host api.Host
	dir  string
}

// New constructs the plugin.
func New(_ context.Context, host api.Host, dir string) (*Plugin, error) {
	return &Plugin{host: host, dir: dir}, nil
}

// Close releases plugin resources.
func (p *Plugin) Close() {}

// Handle dispatches command requests; other request types are ignored.
func (p *Plugin) Handle(ctx context.Context, req api.Request) api.Response {
	if req.Type != "command" {
		return api.Response{Version: 1}
	}
	var ev api.Event
	if len(req.Event) > 0 {
		_ = json.Unmarshal(req.Event, &ev)
	}
	args := req.Args
	if len(args) == 0 {
		p.showCurrent(ctx, ev)
		return api.Response{Version: 1}
	}
	if strings.EqualFold(args[0], "reset") {
		p.setBrand(ctx, ev, "")
		return api.Response{Version: 1}
	}
	p.setBrand(ctx, ev, strings.Join(args, " "))
	return api.Response{Version: 1}
}

// showCurrent displays the current brand name.
func (p *Plugin) showCurrent(ctx context.Context, ev api.Event) {
	r, err := p.host.Call(ctx, api.Call{Method: "get_brand"})
	name := "laowangbot"
	if err == nil && r.Text != "" {
		name = r.Text
	}
	p.editMsg(ctx, ev, "🏷️ <b>当前品牌名</b>\n└ <code>"+html.EscapeString(name)+"</code>\n\n<code>.rename &lt;名字&gt;</code> 修改 · <code>.rename reset</code> 恢复默认")
}

// setBrand updates the brand name via the host.
func (p *Plugin) setBrand(ctx context.Context, ev api.Event, name string) {
	r, err := p.host.Call(ctx, api.Call{Method: "set_brand", Text: name})
	if err != nil {
		p.editMsg(ctx, ev, "❌ <b>修改失败</b>\n└ "+html.EscapeString(err.Error()))
		return
	}
	effective := r.Text
	if effective == "" {
		effective = "laowangbot"
	}
	p.editMsg(ctx, ev, "✅ <b>品牌名已更新</b>\n├ 当前: <code>"+html.EscapeString(effective)+"</code>\n└ .status / .sysinfo 即刻生效，重启不丢失")
}

func (p *Plugin) editMsg(ctx context.Context, ev api.Event, text string) {
	_, _ = p.host.Call(ctx, api.Call{
		Method: "edit", Target: ev.ChatID, MessageID: ev.MessageID, Text: text, HTML: true,
	})
}
