package hostbridge

import (
	"context"
	"encoding/json"
	"github.com/OrionG-hub/laowangbot/internal/bot"
	"github.com/OrionG-hub/laowangbot/pkg/pluginapi"
	"github.com/gotd/td/bin"
	"github.com/gotd/td/tg"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type invoke func(context.Context, bin.Encoder, bin.Decoder) error

func (f invoke) Invoke(c context.Context, i bin.Encoder, o bin.Decoder) error { return f(c, i, o) }
func client(f invoke) *bot.Client {
	p := bot.NewPeerCache()
	u := &tg.User{ID: 7, FirstName: "Me"}
	p.SetSelf(7)
	p.RememberUsers([]tg.UserClass{u})
	return bot.FromAPI(tg.NewClient(f), p, u, slog.Default())
}
func response(out bin.Decoder, in bin.Encoder) error {
	b := &bin.Buffer{}
	if err := in.Encode(b); err != nil {
		return err
	}
	return out.Decode(b)
}
func TestTransport(t *testing.T) {
	calls := 0
	h := NewHandler("secret", []string{"self"}, func(ctx context.Context, c pluginapi.Call) (pluginapi.Result, error) {
		calls++
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("missing timeout")
		}
		return pluginapi.Result{Text: "ok"}, nil
	})
	for _, v := range []struct {
		method, path, body, token string
		status                    int
	}{{"POST", "/call", `{"method":"self"}`, "secret", 200}, {"GET", "/call", "", "secret", 405}, {"POST", "/other", "", "secret", 404}, {"POST", "/call", `{"method":"self"}`, "bad", 401}, {"POST", "/call", `{"method":"send"}`, "secret", 403}, {"POST", "/call", `{"method":"self","bogus":1}`, "secret", 400}, {"POST", "/call", `{"method":"self"}{}`, "secret", 400}, {"POST", "/call", strings.Repeat("x", (1<<20)+1), "secret", 400}} {
		r := httptest.NewRequest(v.method, v.path, strings.NewReader(v.body))
		r.Header.Set("Authorization", "Bearer "+v.token)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != v.status {
			t.Fatalf("%+v got %d", v, w.Code)
		}
	}
	if calls != 1 {
		t.Fatalf("dispatch calls=%d", calls)
	}
}
func TestServerDisconnected(t *testing.T) {
	s, err := Start(func() *bot.Client { return nil }, []string{"self"})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if len(s.Token) != 64 || !strings.HasPrefix(s.URL, "http://127.0.0.1:") {
		t.Fatal("unsafe listener")
	}
	req, _ := http.NewRequest("POST", s.URL+"/call", strings.NewReader(`{"method":"self"}`))
	req.Header.Set("Authorization", "Bearer "+s.Token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 503 {
		t.Fatal(res.Status)
	}
	if _, err := Start(func() *bot.Client { return nil }, []string{"raw"}); err == nil {
		t.Fatal("unknown capability accepted")
	}
}
func TestReadAndMutateFake(t *testing.T) {
	var seen []string
	b := client(func(ctx context.Context, in bin.Encoder, out bin.Decoder) error {
		switch q := in.(type) {
		case *tg.MessagesGetHistoryRequest:
			seen = append(seen, "history")
			if q.Limit != 2 {
				t.Fatal(q.Limit)
			}
			return response(out, &tg.MessagesMessages{Messages: []tg.MessageClass{&tg.Message{ID: 10, PeerID: &tg.PeerUser{UserID: 7}, Message: "hello", Date: 4}}})
		case *tg.MessagesGetMessagesRequest:
			seen = append(seen, "messages")
			return response(out, &tg.MessagesMessages{Messages: []tg.MessageClass{&tg.Message{ID: 10, PeerID: &tg.PeerUser{UserID: 7}}}})
		case *tg.MessagesSendMessageRequest:
			seen = append(seen, "send")
			if q.Message != "hello" {
				t.Fatal(q.Message)
			}
			return response(out, &tg.UpdateShortSentMessage{ID: 42, Date: 4})
		case *tg.MessagesEditMessageRequest:
			seen = append(seen, "edit")
			return response(out, &tg.Updates{})
		case *tg.MessagesDeleteMessagesRequest:
			seen = append(seen, "delete")
			return response(out, &tg.MessagesAffectedMessages{})
		case *tg.MessagesForwardMessagesRequest:
			seen = append(seen, "forward")
			return response(out, &tg.Updates{})
		case *tg.MessagesRequestWebViewRequest:
			seen = append(seen, "webview")
			return response(out, &tg.WebViewResultURL{URL: "https://example.test"})
		case *tg.MessagesRequestSimpleWebViewRequest:
			seen = append(seen, "simple")
			return response(out, &tg.WebViewResultURL{URL: "https://example.test"})
		case *tg.MessagesSendWebViewDataRequest:
			seen = append(seen, "data")
			return response(out, &tg.Updates{})
		default:
			t.Fatalf("unexpected %T", in)
		}
		return nil
	})
	for _, c := range []pluginapi.Call{{Method: "self"}, {Method: "resolve", Target: "me"}, {Method: "identity", Target: "me", User: "me"}, {Method: "history", Target: "me", Limit: 2}, {Method: "messages", Target: "me", IDs: []int{10}}, {Method: "send", Target: "me", Text: "hello"}, {Method: "send", Target: "me", Text: "hello", HTML: true}, {Method: "edit", Target: "me", MessageID: 10, Text: "<safe>"}, {Method: "delete", Target: "me", IDs: []int{10}}, {Method: "forward", Target: "me", User: "me", IDs: []int{10}}, {Method: "webview", Target: "me", URL: "https://example.test"}, {Method: "webview", Target: "me", Simple: true}, {Method: "webview_data", Target: "me", Data: "{}"}} {
		r, err := dispatch(context.Background(), b, c)
		if err != nil {
			t.Fatalf("%s: %v", c.Method, err)
		}
		if c.Method == "send" && r.MessageID != 42 {
			t.Fatal(r)
		}
	}
	if len(seen) != 10 {
		t.Fatal(seen)
	}
}
func TestValidation(t *testing.T) {
	b := client(func(context.Context, bin.Encoder, bin.Decoder) error { t.Fatal("RPC called"); return nil })
	for _, c := range []pluginapi.Call{{Method: "send"}, {Method: "history", Target: "me", Limit: 101}, {Method: "messages", Target: "me"}, {Method: "delete", Target: "me", IDs: []int{-1}}, {Method: "edit", Target: "me"}, {Method: "download", Target: "me"}, {Method: "identity", Target: "me"}, {Method: "unknown", Target: "me"}} {
		if _, err := dispatch(context.Background(), b, c); err == nil {
			t.Fatal(c)
		}
	}
}
func TestSerializationAndClick(t *testing.T) {
	m := &tg.Message{ID: 8, PeerID: &tg.PeerChannel{ChannelID: 44}, FromID: &tg.PeerUser{UserID: 9}, Date: 3, Message: "😀https://x", Entities: []tg.MessageEntityClass{&tg.MessageEntityURL{Offset: 2, Length: 9}}, ReplyMarkup: &tg.ReplyInlineMarkup{Rows: []tg.KeyboardInlineButtonRow{{Buttons: []tg.KeyboardInlineButton{{Text: "Go", Type: &tg.InlineButtonTypeURL{URL: "https://x"}}, {Text: "Do", Type: &tg.InlineButtonTypeCallback{Data: []byte("data")}}}}}}}
	m.SetFwdFrom(tg.MessageFwdHeader{FromID: &tg.PeerChannel{ChannelID: 77}, ChannelPost: 6, Date: 2})
	r := serialize(m)
	if r.ChatID != "-10044" || r.ForwardID != "channel:77" || len(r.URLs) != 1 || r.URLs[0] != "https://x" || len(r.Buttons) != 2 {
		t.Fatalf("%+v", r)
	}
	b := client(func(ctx context.Context, in bin.Encoder, out bin.Decoder) error {
		q := in.(*tg.MessagesGetBotCallbackAnswerRequest)
		if string(q.Data) != "data" || q.MsgID != 8 {
			t.Fatal(q)
		}
		return response(out, &tg.MessagesBotCallbackAnswer{Message: "done"})
	})
	v, err := click(context.Background(), b, &tg.InputPeerSelf{}, m, 0, 1)
	if err != nil || v.Text != "done" {
		t.Fatal(v, err)
	}
	v, err = click(context.Background(), b, &tg.InputPeerSelf{}, m, 0, 0)
	if err != nil || v.URL != "https://x" {
		t.Fatal(v, err)
	}
	if _, err = click(context.Background(), b, &tg.InputPeerSelf{}, m, -1, 0); err == nil {
		t.Fatal("invalid index")
	}
}
func TestResponseLimit(t *testing.T) {
	h := NewHandler("s", []string{"download"}, func(context.Context, pluginapi.Call) (pluginapi.Result, error) {
		return pluginapi.Result{Bytes: make([]byte, MaxMedia+1)}, nil
	})
	req := httptest.NewRequest("POST", "/call", strings.NewReader(`{"method":"download"}`))
	req.Header.Set("Authorization", "Bearer s")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 413 {
		t.Fatal(w.Code)
	}
	data, _ := io.ReadAll(w.Result().Body)
	var r pluginapi.Result
	if json.Unmarshal(data, &r) != nil || r.Error == "" {
		t.Fatal(string(data))
	}
}

func TestIdentitySelfAndCreators(t *testing.T) {
	b := client(func(_ context.Context, in bin.Encoder, out bin.Decoder) error {
		switch in.(type) {
		case *tg.ChannelsGetParticipantRequest:
			return response(out, &tg.ChannelsChannelParticipant{Participant: &tg.ChannelParticipantCreator{UserID: 8}})
		case *tg.MessagesGetFullChatRequest:
			return response(out, &tg.MessagesChatFull{FullChat: &tg.ChatFull{ID: 5, Participants: &tg.ChatParticipants{ChatID: 5, Participants: []tg.ChatParticipantClass{&tg.ChatParticipantCreator{UserID: 8}}}, ChatPhoto: &tg.PhotoEmpty{}, NotifySettings: tg.PeerNotifySettings{}}})
		}
		t.Fatalf("unexpected %T", in)
		return nil
	})
	u := &tg.User{ID: 8}
	u.SetAccessHash(1)
	b.Peers().RememberUsers([]tg.UserClass{u})
	for _, tc := range []struct {
		p          tg.InputPeerClass
		user, want string
	}{{&tg.InputPeerSelf{}, "me", "owner"}, {&tg.InputPeerChannel{ChannelID: 5}, "8", "admin"}, {&tg.InputPeerChat{ChatID: 5}, "8", "admin"}} {
		got, e := identity(context.Background(), b, tc.p, tc.user)
		if e != nil || got != tc.want {
			t.Fatalf("%s got %s err %v", tc.want, got, e)
		}
	}
}
func TestMissingSenderFallback(t *testing.T) {
	b := client(func(_ context.Context, in bin.Encoder, out bin.Decoder) error {
		return response(out, &tg.MessagesMessages{Messages: []tg.MessageClass{&tg.Message{ID: 1, PeerID: &tg.PeerUser{UserID: 8}}, &tg.Message{ID: 2, PeerID: &tg.PeerUser{UserID: 8}, Out: true}, &tg.Message{ID: 3, PeerID: &tg.PeerUser{UserID: 7}}}})
	})
	r, e := dispatch(context.Background(), b, pluginapi.Call{Method: "messages", Target: "me", IDs: []int{1, 2, 3}})
	if e != nil {
		t.Fatal(e)
	}
	if r.Messages[0].SenderID != "8" || r.Messages[1].SenderID != "7" || r.Messages[2].SenderID != "7" || !r.Messages[2].Out {
		t.Fatal(r.Messages)
	}
}
func TestGroupWebviewBotAndPeer(t *testing.T) {
	b := client(func(_ context.Context, in bin.Encoder, out bin.Decoder) error {
		q := in.(*tg.MessagesRequestWebViewRequest)
		if q.Peer.(*tg.InputPeerChat).ChatID != 5 || q.Bot.(*tg.InputUser).UserID != 8 {
			t.Fatal(q)
		}
		return response(out, &tg.WebViewResultURL{URL: "https://app.test"})
	})
	u := &tg.User{ID: 8, Bot: true}
	u.SetAccessHash(1)
	b.Peers().RememberUsers([]tg.UserClass{u})
	for _, via := range []bool{false, true} {
		m := &tg.Message{ID: 4, FromID: &tg.PeerUser{UserID: 8}, ReplyMarkup: &tg.ReplyInlineMarkup{Rows: []tg.KeyboardInlineButtonRow{{Buttons: []tg.KeyboardInlineButton{{Text: "app", Type: &tg.InlineButtonTypeWebView{URL: "https://app.test"}}}}}}}
		if via {
			m.FromID = &tg.PeerUser{UserID: 9}
			m.SetViaBotID(8)
		}
		r, e := click(context.Background(), b, &tg.InputPeerChat{ChatID: 5}, m, 0, 0)
		if e != nil || r.URL != "https://app.test" {
			t.Fatal(r, e)
		}
	}
}
func TestResolvePublicLinks(t *testing.T) {
	b := client(func(_ context.Context, in bin.Encoder, out bin.Decoder) error {
		q := in.(*tg.ContactsResolveUsernameRequest)
		if q.Username != "example" {
			t.Fatal(q.Username)
		}
		u := &tg.User{ID: 8, Username: "example"}
		u.SetAccessHash(1)
		return response(out, &tg.ContactsResolvedPeer{Peer: &tg.PeerUser{UserID: 8}, Users: []tg.UserClass{u}})
	})
	for _, s := range []string{"https://t.me/example", "https://telegram.me/example/123", "t.me/example", "@example"} {
		r, e := dispatch(context.Background(), b, pluginapi.Call{Method: "resolve", Target: s})
		if e != nil || r.Entity.ID != "8" {
			t.Fatal(s, r, e)
		}
	}
}
