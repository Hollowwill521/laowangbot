package dme

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/OrionG-hub/laowangbot/internal/app"
	"github.com/OrionG-hub/laowangbot/internal/bot"
	"github.com/OrionG-hub/laowangbot/internal/command"
	"github.com/gotd/td/bin"
	"github.com/gotd/td/tg"
)

type rpcFake struct {
	dialogSlices  bool
	historyFail   map[int64]bool
	historyLimits []int
	t             *testing.T
	history       map[int64][]tg.MessageClass
	replies       []tg.MessageClass
	dialogs       map[int][]tg.DialogClass
	calls         []string
	edits         []int
	deleted       []int
	notices       []string
	editTimes     []time.Time
	deleteTimes   []time.Time
	cancel        context.CancelFunc
	cancelOn      string
	editFail      bool
	deleteFail    map[int]bool
}

func peerNumber(p tg.InputPeerClass) int64 {
	switch p := p.(type) {
	case *tg.InputPeerChat:
		return p.ChatID
	case *tg.InputPeerChannel:
		return p.ChannelID
	case *tg.InputPeerUser:
		return p.UserID
	case *tg.InputPeerSelf:
		return 1
	}
	return 0
}
func (f *rpcFake) Invoke(ctx context.Context, input bin.Encoder, output bin.Decoder) error {
	var req bin.Buffer
	if err := input.Encode(&req); err != nil {
		return fmt.Errorf("encode %T: %w", input, err)
	}
	respond := func(v bin.Encoder) error {
		var b bin.Buffer
		if err := v.Encode(&b); err != nil {
			return err
		}
		return output.Decode(&b)
	}
	record := func(s string) {
		f.calls = append(f.calls, s)
		if s == f.cancelOn && f.cancel != nil {
			f.cancel()
		}
	}
	switch r := input.(type) {
	case *tg.MessagesGetHistoryRequest:
		record("history")
		f.historyLimits = append(f.historyLimits, r.Limit)
		if f.historyFail[peerNumber(r.Peer)] {
			return errors.New("HISTORY_FAILED")
		}
		var list []tg.MessageClass
		for _, m := range f.history[peerNumber(r.Peer)] {
			id := m.GetID()
			if (r.OffsetID == 0 || id < r.OffsetID) && (r.MaxID == 0 || id < r.MaxID) && (r.MinID == 0 || id > r.MinID) {
				list = append(list, m)
			}
		}
		if len(list) > r.Limit {
			list = list[:r.Limit]
		}
		return respond(&tg.MessagesMessages{Messages: list})
	case *tg.MessagesSearchRequest:
		record("search")
		return respond(&tg.MessagesMessages{Messages: f.history[peerNumber(r.Peer)]})
	case *tg.MessagesGetMessagesRequest:
		record("reply")
		return respond(&tg.MessagesMessages{Messages: f.replies})
	case *tg.ChannelsGetMessagesRequest:
		record("reply")
		return respond(&tg.MessagesMessages{Messages: f.replies})
	case *tg.MessagesGetDialogsRequest:
		record(fmt.Sprintf("dialogs%d", r.FolderID))
		if f.dialogSlices {
			all := f.dialogs[r.FolderID]
			start := 0
			if r.OffsetID != 0 {
				for i, v := range all {
					if d, ok := v.(*tg.Dialog); ok && bot.PeerID(d.Peer) == fmt.Sprintf("-%d", peerNumber(r.OffsetPeer)) {
						start = i + 1
						break
					}
				}
			}
			end := min(len(all), start+r.Limit)
			return respond(&tg.MessagesDialogsSlice{Count: len(all), Dialogs: all[start:end]})
		}
		return respond(&tg.MessagesDialogs{Dialogs: f.dialogs[r.FolderID]})
	case *tg.UploadSaveFilePartRequest:
		record("upload")
		return respond(&tg.BoolTrue{})
	case *tg.MessagesEditMessageRequest:
		if r.Media != nil {
			record("media")
			f.edits = append(f.edits, r.ID)
			f.editTimes = append(f.editTimes, time.Now())
			if _, ok := r.Media.(*tg.InputMediaUploadedPhoto); !ok || r.Message != "" {
				f.t.Fatalf("incorrect scrub: %+v", r)
			}
			if f.editFail {
				return errors.New("EDIT_FAILED")
			}
		} else {
			record("text")
			f.notices = append(f.notices, r.Message)
		}
		return respond(&tg.Updates{})
	case *tg.MessagesDeleteMessagesRequest:
		record("delete")
		if !r.Revoke {
			f.t.Fatal("delete must revoke")
		}
		for _, id := range r.ID {
			if f.deleteFail[id] {
				return errors.New("DELETE_FAILED")
			}
		}
		f.deleted = append(f.deleted, r.ID...)
		f.deleteTimes = append(f.deleteTimes, time.Now())
		return respond(&tg.MessagesAffectedMessages{})
	case *tg.ChannelsDeleteMessagesRequest:
		record("delete")
		f.deleted = append(f.deleted, r.ID...)
		f.deleteTimes = append(f.deleteTimes, time.Now())
		return respond(&tg.MessagesAffectedMessages{})
	case *tg.UpdatesGetStateRequest:
		return respond(&tg.UpdatesState{})
	case *tg.MessagesSendMessageRequest:
		record("notice")
		f.notices = append(f.notices, r.Message)
		return respond(&tg.UpdateShortSentMessage{ID: 777, Date: 1})
	default:
		return fmt.Errorf("unexpected RPC %T", input)
	}
}
func message(id int, own bool, media bool) *tg.Message {
	m := &tg.Message{ID: id, Out: own, PeerID: &tg.PeerChat{ChatID: 9}, Date: int(time.Now().Unix()), Message: "original"}
	sender := int64(2)
	if own {
		sender = 1
	}
	m.SetFromID(&tg.PeerUser{UserID: sender})
	if media {
		m.SetMedia(&tg.MessageMediaPhoto{Photo: &tg.PhotoEmpty{ID: int64(id)}})
	}
	return m
}
func fixture(t *testing.T, f *rpcFake, args []string, reply int) (*command.Command, *command.Invocation, string) {
	t.Helper()
	f.t = t
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "data", "dme"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "data", "dme", "dme_troll_image.png"), []byte("placeholder fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	peers := bot.NewPeerCache()
	peers.SetSelf(1)
	c := bot.FromAPI(tg.NewClient(f), peers, &tg.User{ID: 1}, logger)
	reg := command.New([]string{"!"}, logger)
	Register(&app.App{Root: root, Registry: reg, Logger: logger})
	cmd, _ := reg.Lookup("dme")
	inv := &command.Invocation{Prefix: "!", Args: args, Client: c, Log: logger, Message: &bot.Message{ID: 100, Peer: &tg.PeerChat{ChatID: 9}, ChatID: "-9", ReplyToID: reply, Raw: &tg.Message{Date: int(time.Now().Unix())}}}
	return cmd, inv, root
}
func TestDefaultMediaOnlySequential(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := &rpcFake{history: map[int64][]tg.MessageClass{9: {message(99, true, true), message(98, true, false), message(97, true, true), message(96, false, true)}}}
		cmd, inv, _ := fixture(t, f, []string{"3"}, 0)
		if err := cmd.Handle(context.Background(), inv); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(f.edits, []int{99, 97}) {
			t.Fatalf("default media edits: %v; calls %v", f.edits, f.calls)
		}
		if !reflect.DeepEqual(f.deleted, []int{100, 99, 98, 97}) {
			t.Fatalf("deleted %v", f.deleted)
		}
		if f.editTimes[1].Sub(f.editTimes[0]) < 1500*time.Millisecond {
			t.Fatal("missing sequential delay")
		}
		if f.deleteTimes[len(f.deleteTimes)-1].Sub(f.editTimes[1]) < 2500*time.Millisecond {
			t.Fatal("missing settle delay")
		}
	})
}
func TestReplyAlbumAndRange(t *testing.T) {
	for _, rangeMode := range []bool{false, true} {
		t.Run(fmt.Sprint(rangeMode), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				a, b, c := message(90, true, false), message(89, true, false), message(88, false, false)
				a.SetGroupedID(7)
				b.SetGroupedID(7)
				c.SetGroupedID(7)
				outOnly := message(92, false, false)
				outOnly.Out = true
				f := &rpcFake{replies: []tg.MessageClass{a}, history: map[int64][]tg.MessageClass{9: {message(101, true, false), message(99, true, false), outOnly, a, b, c}}}
				args := []string{}
				if rangeMode {
					args = []string{"-r"}
				}
				cmd, inv, _ := fixture(t, f, args, 90)
				if err := cmd.Handle(context.Background(), inv); err != nil {
					t.Fatal(err)
				}
				want := []int{100, 90, 89}
				if rangeMode {
					want = []int{100, 99, 90}
				}
				if !reflect.DeepEqual(f.deleted, want) {
					t.Fatalf("got %v want %v calls %v", f.deleted, want, f.calls)
				}
			})
		})
	}
}
func TestInvalidArgumentsNeverDelete(t *testing.T) {
	for _, args := range [][]string{{}, {"0"}, {"-f", "1"}, {"1", "2"}, {"-r"}, {"-a", "-r", "1"}, {"bogus"}, {"help"}} {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			f := &rpcFake{}
			cmd, inv, _ := fixture(t, f, args, 0)
			_ = cmd.Handle(context.Background(), inv)
			if len(f.deleted) > 0 {
				t.Fatalf("invalid command deletes: %v", f.deleted)
			}
		})
	}
}

func TestGlobalLatestAcrossArchiveAndCutoff(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a, b, future, equal, foreign := message(70, true, false), message(60, true, false), message(80, true, false), message(79, true, false), message(90, false, false)
		a.Date -= 20
		b.Date -= 10
		future.Date++
		foreign.Date -= 2
		for _, m := range []*tg.Message{b, future, equal, foreign} {
			m.PeerID = &tg.PeerChat{ChatID: 10}
		}
		f := &rpcFake{history: map[int64][]tg.MessageClass{9: {a}, 10: {foreign, future, equal, b}}, dialogs: map[int][]tg.DialogClass{0: {&tg.Dialog{Peer: &tg.PeerChat{ChatID: 9}, TopMessage: 100}}, 1: {&tg.Dialog{Peer: &tg.PeerChat{ChatID: 9}, TopMessage: 100}, &tg.Dialog{Peer: &tg.PeerChat{ChatID: 10}, TopMessage: 90}}}}
		cmd, inv, _ := fixture(t, f, []string{"--all", "2"}, 0)
		if err := cmd.Handle(context.Background(), inv); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(f.deleted, []int{100, 60, 70}) {
			t.Fatalf("global selection %v calls %v", f.deleted, f.calls)
		}
		if strings.Count(strings.Join(f.calls, ","), "history") != 2 {
			t.Fatalf("dialog not deduplicated: %v", f.calls)
		}
	})
}
func TestForeignAndCrossChatReplyNeverDeletes(t *testing.T) {
	for _, cross := range []bool{false, true} {
		t.Run(fmt.Sprint(cross), func(t *testing.T) {
			f := &rpcFake{replies: []tg.MessageClass{message(90, false, true)}}
			cmd, inv, _ := fixture(t, f, nil, 90)
			if cross {
				inv.Message.ReplyToPeer = &tg.PeerChat{ChatID: 10}
			}
			if err := cmd.Handle(context.Background(), inv); err != nil {
				t.Fatal(err)
			}
			if len(f.deleted) > 0 || len(f.edits) > 0 {
				t.Fatalf("foreign mutation %v %v", f.deleted, f.edits)
			}
		})
	}
}
func TestMediaSkipRulesAndMissingPlaceholder(t *testing.T) {
	for _, missing := range []bool{false, true} {
		t.Run(fmt.Sprint(missing), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				forwarded, old, web, sticker := message(99, true, true), message(98, true, true), message(97, true, true), message(96, true, true)
				forwarded.SetFwdFrom(tg.MessageFwdHeader{Date: 1})
				old.Date -= 172801
				web.SetMedia(&tg.MessageMediaWebPage{Webpage: &tg.WebPageEmpty{ID: 1}})
				sticker.SetMedia(&tg.MessageMediaDocument{Document: &tg.Document{ID: 1, MimeType: "image/webp", Thumbs: []tg.PhotoSizeClass{}, Attributes: []tg.DocumentAttributeClass{&tg.DocumentAttributeSticker{Alt: "x", Stickerset: &tg.InputStickerSetEmpty{}}}}})
				f := &rpcFake{history: map[int64][]tg.MessageClass{9: {forwarded, old, web, sticker, message(95, true, true)}}}
				cmd, inv, root := fixture(t, f, []string{"5"}, 0)
				if missing {
					if err := os.Remove(filepath.Join(root, "data", "dme", "dme_troll_image.png")); err != nil {
						t.Fatal(err)
					}
				}
				if err := cmd.Handle(context.Background(), inv); err != nil {
					t.Fatal(err)
				}
				want := []int{95}
				if missing {
					want = nil
				}
				if !reflect.DeepEqual(f.edits, want) {
					t.Fatalf("edited %v", f.edits)
				}
				if !reflect.DeepEqual(f.deleted, []int{100, 99, 98, 97, 96, 95}) {
					t.Fatalf("deleted %v", f.deleted)
				}
				if missing && !strings.Contains(strings.Join(f.notices, ""), "占位图") {
					t.Fatalf("missing image must be visible: %v", f.notices)
				}
			})
		})
	}
}
func TestEditFailureStillDeletesAndReports(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := &rpcFake{editFail: true, history: map[int64][]tg.MessageClass{9: {message(99, true, true)}}}
		cmd, inv, _ := fixture(t, f, []string{"1"}, 0)
		if err := cmd.Handle(context.Background(), inv); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(f.deleted, []int{100, 99}) {
			t.Fatal(f.deleted)
		}
		text := strings.Join(f.notices, "")
		if !strings.Contains(text, "成功替换媒体 0") || !strings.Contains(text, "编辑失败 1") {
			t.Fatal(text)
		}
	})
}
func TestCancellationStopsFurtherMutation(t *testing.T) {
	for _, at := range []string{"before", "history", "upload", "media"} {
		t.Run(at, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				f := &rpcFake{cancel: cancel, cancelOn: at, history: map[int64][]tg.MessageClass{9: {message(99, true, true)}}}
				cmd, inv, _ := fixture(t, f, []string{"1"}, 0)
				if at == "before" {
					cancel()
				}
				if err := cmd.Handle(ctx, inv); !errors.Is(err, context.Canceled) {
					t.Fatalf("got %v", err)
				}
				want := []int{100}
				if at == "before" {
					want = nil
				}
				if !reflect.DeepEqual(f.deleted, want) {
					t.Fatalf("deletion after cancellation %v calls%v", f.deleted, f.calls)
				}
				if at == "upload" && len(f.edits) > 0 {
					t.Fatal("edited after canceled upload")
				}
			})
		})
	}
}
func TestAdaptiveDeleteDoesNotSkipIDsAndCountsFailures(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := &rpcFake{deleteFail: map[int]bool{7: true}}
		_, inv, _ := fixture(t, f, nil, 0)
		r, err := newRun(context.Background(), inv, dmeConfig{BatchSize: 5, SearchLimit: 100, RetryAttempts: 0}, "")
		if err != nil {
			t.Fatal(err)
		}
		ids := []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17}
		if err := r.deleteBatch(r.peer, ids); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(f.deleted, []int{1, 2, 3, 4, 5, 6, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17}) || r.deleted != 16 || r.failed != 1 {
			t.Fatalf("deleted%v counts%d/%d", f.deleted, r.deleted, r.failed)
		}
	})
}
func TestCountAbove2000AndUnlimitedAreStreamed(t *testing.T) {
	for _, count := range []string{"2001", "999999"} {
		t.Run(count, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				var history []tg.MessageClass
				for id := 2100; id > 0; id-- {
					history = append(history, message(id, true, false))
				}
				f := &rpcFake{history: map[int64][]tg.MessageClass{9: history}}
				cmd, inv, _ := fixture(t, f, []string{count}, 0)
				inv.Message.ID = 3000
				if err := cmd.Handle(context.Background(), inv); err != nil {
					t.Fatal(err)
				}
				want := 2002
				if count == "999999" {
					want = 2101
				}
				if len(f.deleted) != want {
					t.Fatalf("deleted%d want%d", len(f.deleted), want)
				}
			})
		})
	}
}
func TestCountHonorsOutButNotChannelIdentity(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		out := message(99, false, false)
		out.Out = true
		foreign := message(98, false, false)
		foreign.SetFromID(&tg.PeerChannel{ChannelID: 9})
		f := &rpcFake{history: map[int64][]tg.MessageClass{9: {out, foreign, message(97, true, false)}}}
		cmd, inv, _ := fixture(t, f, []string{"999999"}, 0)
		if err := cmd.Handle(context.Background(), inv); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(f.deleted, []int{100, 99, 97}) {
			t.Fatal(f.deleted)
		}
	})
}
func TestConflictingReplyArgumentsNeverDelete(t *testing.T) {
	for _, args := range [][]string{{"1"}, {"-a", "1"}, {"-r", "1"}, {"-a", "-r"}, {"-a", "--all", "1"}} {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			f := &rpcFake{}
			cmd, inv, _ := fixture(t, f, args, 90)
			_ = cmd.Handle(context.Background(), inv)
			if len(f.deleted) > 0 {
				t.Fatal(f.deleted)
			}
		})
	}
}
func TestRangeCanAnchorForeignMessageButDeletesOnlyOwn(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		anchor := message(90, false, false)
		f := &rpcFake{replies: []tg.MessageClass{anchor}, history: map[int64][]tg.MessageClass{9: {message(99, true, false), anchor, message(89, true, false)}}}
		cmd, inv, _ := fixture(t, f, []string{"-r"}, 90)
		if err := cmd.Handle(context.Background(), inv); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(f.deleted, []int{100, 99}) {
			t.Fatal(f.deleted)
		}
	})
}
func TestGlobalPaginates500PerFolderAnd80History(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := &rpcFake{dialogSlices: true, dialogs: map[int][]tg.DialogClass{}, history: map[int64][]tg.MessageClass{}}
		for folder := 0; folder < 2; folder++ {
			for i := 0; i < 550; i++ {
				id := int64(1000 + folder*1000 + i)
				f.dialogs[folder] = append(f.dialogs[folder], &tg.Dialog{Peer: &tg.PeerChat{ChatID: id}, TopMessage: 50})
				m := message(50, true, false)
				m.Date -= 100
				m.PeerID = &tg.PeerChat{ChatID: id}
				f.history[id] = []tg.MessageClass{m}
			}
		}
		cmd, inv, _ := fixture(t, f, []string{"-a", "999999"}, 0)
		if err := cmd.Handle(context.Background(), inv); err != nil {
			t.Fatal(err)
		}
		if len(f.deleted) != 1001 {
			t.Fatalf("deleted %d want1001", len(f.deleted))
		}
		if len(f.historyLimits) != 1000 {
			t.Fatalf("scanned %d dialogs", len(f.historyLimits))
		}
		for _, n := range f.historyLimits {
			if n != 80 {
				t.Fatal(n)
			}
		}
	})
}
func TestAlbumLookupFailureFallsBackToOwnReply(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a := message(90, true, false)
		a.SetGroupedID(7)
		f := &rpcFake{historyFail: map[int64]bool{9: true}, replies: []tg.MessageClass{a}}
		cmd, inv, _ := fixture(t, f, nil, 90)
		if err := cmd.Handle(context.Background(), inv); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(f.deleted, []int{100, 90}) {
			t.Fatal(f.deleted)
		}
	})
}
func TestGlobalSkipsFailedDialogAndReports(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		m := message(90, true, false)
		m.PeerID = &tg.PeerChat{ChatID: 10}
		m.Date--
		f := &rpcFake{historyFail: map[int64]bool{9: true}, history: map[int64][]tg.MessageClass{10: {m}}, dialogs: map[int][]tg.DialogClass{0: {&tg.Dialog{Peer: &tg.PeerChat{ChatID: 9}, TopMessage: 100}, &tg.Dialog{Peer: &tg.PeerChat{ChatID: 10}, TopMessage: 90}}}}
		cmd, inv, _ := fixture(t, f, []string{"-a", "1"}, 0)
		if err := cmd.Handle(context.Background(), inv); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(f.deleted, []int{100, 90}) {
			t.Fatal(f.deleted)
		}
		if !strings.Contains(strings.Join(f.notices, ""), "扫描失败 1") {
			t.Fatal(f.notices)
		}
	})
}

func TestArgumentScope(t *testing.T) {
	for _, tc := range []struct {
		args  []string
		reply bool
		want  options
		bad   bool
	}{
		{nil, false, options{help: true}, false}, {nil, true, options{reply: true}, false},
		{[]string{"h"}, true, options{reply: true, help: true}, false},
		{[]string{"--help"}, false, options{help: true}, false},
		{[]string{"999999"}, false, options{count: 999999}, false},
		{[]string{"999"}, false, options{count: 999}, false},
		{[]string{"3", "--all"}, false, options{count: 3, global: true}, false},
		{[]string{"-r"}, true, options{reply: true, span: true}, false},
		{[]string{"-r", "-r"}, true, options{}, true},
		{[]string{"-a", "--all", "1"}, false, options{}, true},
		{[]string{"99999999999999999999999999"}, false, options{}, true},
		{[]string{"+2"}, false, options{}, true}, {[]string{"-1"}, false, options{}, true},
	} {
		got, err := parse(tc.args, tc.reply)
		if tc.bad {
			if err == nil {
				t.Fatalf("accepted %v", tc.args)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Fatalf("%v: %+v %v", tc.args, got, err)
		}
	}
}
func TestConfigurationBounds(t *testing.T) {
	c := dmeConfig{BatchSize: 1, SearchLimit: 999, RetryAttempts: -1}
	c.normalize()
	if c != dmeDefaults() {
		t.Fatal(c)
	}
	c = dmeConfig{BatchSize: 100, SearchLimit: 10, RetryAttempts: 0}
	c.normalize()
	if c.BatchSize != 100 || c.SearchLimit != 10 || c.RetryAttempts != 0 {
		t.Fatal(c)
	}
}
func TestCancellationDuringSettleDoesNotDeleteMedia(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		f := &rpcFake{history: map[int64][]tg.MessageClass{9: {message(99, true, true)}}}
		cmd, inv, _ := fixture(t, f, []string{"1"}, 0)
		go func() { time.Sleep(2 * time.Second); cancel() }()
		if err := cmd.Handle(ctx, inv); !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(f.deleted, []int{100}) || !reflect.DeepEqual(f.edits, []int{99}) {
			t.Fatalf("edits%v deletes%v", f.edits, f.deleted)
		}
	})
}
func TestSparseHistoryStopsAfterThreeEmptyPages(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var history []tg.MessageClass
		for id := 499; id > 100; id-- {
			history = append(history, message(id, false, false))
		}
		history = append(history, message(90, true, false))
		f := &rpcFake{history: map[int64][]tg.MessageClass{9: history}}
		cmd, inv, _ := fixture(t, f, []string{"1"}, 0)
		inv.Message.ID = 500
		if err := cmd.Handle(context.Background(), inv); err != nil {
			t.Fatal(err)
		}
		if len(f.historyLimits) != 3 || !reflect.DeepEqual(f.deleted, []int{500}) {
			t.Fatalf("scans%v deletes%v", f.historyLimits, f.deleted)
		}
	})
}
