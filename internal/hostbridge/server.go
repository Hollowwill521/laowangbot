// Package hostbridge exposes a bounded, capability-scoped local Telegram API.
package hostbridge

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/OrionG-hub/laowangbot/internal/bot"
	"github.com/OrionG-hub/laowangbot/pkg/pluginapi"
)

const MaxMedia = 8 << 20

var methods = map[string]bool{"self": true, "resolve": true, "identity": true, "history": true, "messages": true, "send": true, "send_file": true, "send_photo": true, "chat_action": true, "folders": true, "user_info": true, "edit": true, "delete": true, "forward": true, "download": true, "click": true, "webview": true, "webview_data": true}
var errDisconnected = errors.New("Telegram is not connected")

type Server struct {
	URL, Token string
	server     *http.Server
}

func (s *Server) Close() error { return s.server.Close() }
func Start(client func() *bot.Client, capabilities []string) (*Server, error) {
	for _, c := range capabilities {
		if !methods[c] {
			return nil, errors.New("unknown host capability: " + c)
		}
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, err
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	s := &Server{URL: "http://" + listener.Addr().String(), Token: hex.EncodeToString(secret)}
	s.server = &http.Server{Handler: NewHandler(s.Token, capabilities, func(ctx context.Context, c pluginapi.Call) (pluginapi.Result, error) {
		b := client()
		if b == nil || b.Self() == nil {
			return pluginapi.Result{}, errDisconnected
		}
		return dispatch(ctx, b, c)
	}), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 50 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 8192}
	go s.server.Serve(listener)
	return s, nil
}

// NewHandler builds the authenticated transport; dispatchers never receive denied calls.
func NewHandler(token string, capabilities []string, call func(context.Context, pluginapi.Call) (pluginapi.Result, error)) http.Handler {
	allowed := map[string]bool{}
	for _, c := range capabilities {
		allowed[c] = methods[c]
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fail := func(code int, msg string) {
			w.WriteHeader(code)
			_ = json.NewEncoder(w).Encode(pluginapi.Result{Error: msg})
		}
		if r.URL.Path != "/call" {
			fail(404, "not found")
			return
		}
		if r.Method != http.MethodPost {
			fail(405, "POST required")
			return
		}
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+token)) != 1 || token == "" {
			fail(401, "unauthorized")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		defer r.Body.Close()
		var c pluginapi.Call
		d := json.NewDecoder(r.Body)
		d.DisallowUnknownFields()
		if err := d.Decode(&c); err != nil {
			fail(400, "invalid request")
			return
		}
		if d.Decode(new(any)) != io.EOF {
			fail(400, "invalid request")
			return
		}
		if !allowed[c.Method] {
			fail(403, "capability denied")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
		defer cancel()
		result, err := call(ctx, c)
		if err != nil {
			if errors.Is(err, errDisconnected) {
				fail(503, err.Error())
			} else {
				fail(400, err.Error())
			}
			return
		}
		if len(result.Bytes) > MaxMedia {
			fail(413, "media exceeds 8 MiB")
			return
		}
		data, err := json.Marshal(result)
		if err != nil || len(data) > 12<<20 {
			fail(413, "response too large")
			return
		}
		_, _ = w.Write(data)
	})
}
