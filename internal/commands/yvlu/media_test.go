package yvlu

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/OrionG-hub/laowangbot/internal/bot"
	"github.com/OrionG-hub/laowangbot/internal/command"
	"github.com/gotd/td/bin"
	"github.com/gotd/td/tg"
)

type stickerRPC struct {
	data      []byte
	fail      bool
	downloads int
}

func (f *stickerRPC) Invoke(_ context.Context, input bin.Encoder, output bin.Decoder) error {
	request, ok := input.(*tg.UploadGetFileRequest)
	if !ok {
		return errors.New("unexpected RPC")
	}
	f.downloads++
	if f.fail {
		return errors.New("media unavailable")
	}
	data := f.data
	if int(request.Offset) >= len(data) {
		data = nil
	} else {
		data = data[int(request.Offset):]
	}
	var buffer bin.Buffer
	response := &tg.UploadFile{Type: &tg.StorageFileUnknown{}, Bytes: data}
	if err := response.Encode(&buffer); err != nil {
		return err
	}
	return output.Decode(&buffer)
}
func stickerInvocation(f *stickerRPC, mime string, size int64) (*command.Invocation, *bot.Message) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	client := bot.FromAPI(tg.NewClient(f), bot.NewPeerCache(), &tg.User{ID: 1}, logger)
	doc := &tg.Document{ID: 42, AccessHash: 43, FileReference: []byte{1}, MimeType: mime, Size: size, DCID: 2, Attributes: []tg.DocumentAttributeClass{&tg.DocumentAttributeSticker{Alt: "🙂", Stickerset: &tg.InputStickerSetEmpty{}}}}
	raw := &tg.Message{ID: 5, PeerID: &tg.PeerUser{UserID: 1}}
	raw.SetMedia(&tg.MessageMediaDocument{Document: doc})
	return &command.Invocation{Client: client, Log: logger}, &bot.Message{Raw: raw}
}
func TestQuotedStickerMediaIsNotDiscarded(t *testing.T) {
	for _, mime := range []string{"video/webm", "image/webp"} {
		t.Run(mime, func(t *testing.T) {
			data := []byte("fixture-sticker-payload")
			f := &stickerRPC{data: data}
			inv, msg := stickerInvocation(f, mime, int64(len(data)))
			item := quoteMessage{}
			err := (&yvluService{}).describeMedia(t.Context(), inv, msg, &item)
			want := "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data)
			if err != nil {
				t.Fatal(err)
			}
			if item.Media == nil || item.Media.URL != want {
				t.Fatalf("media discarded: %+v", item)
			}
			if item.MediaType != "" || f.downloads == 0 {
				t.Fatal(item.MediaType, f.downloads)
			}
		})
	}
}
func TestStickerFailureReturnsVisibleError(t *testing.T) {
	for _, size := range []int64{10, 9 << 20} {
		f := &stickerRPC{fail: true}
		inv, msg := stickerInvocation(f, "video/webm", size)
		item := quoteMessage{}
		err := (&yvluService{}).describeMedia(t.Context(), inv, msg, &item)
		if item.Media != nil || err == nil || !strings.Contains(err.Error(), "贴纸") {
			t.Fatalf("blank fallback: %+v", item)
		}
		if size > 8<<20 && f.downloads != 0 {
			t.Fatal("oversize downloaded")
		}
	}
}
func TestStickerCaptionPreservedOnFailure(t *testing.T) {
	inv, msg := stickerInvocation(&stickerRPC{fail: true}, "video/webm", 10)
	item := quoteMessage{Text: "original caption"}
	(&yvluService{}).describeMedia(t.Context(), inv, msg, &item)
	if item.Text != "original caption" {
		t.Fatal(item.Text)
	}
}

func TestBrokenTGSReturnsErrorInsteadOfEmptyQuote(t *testing.T) {
	inv, msg := stickerInvocation(&stickerRPC{data: []byte("broken tgs")}, "application/x-tgsticker", 10)
	item := quoteMessage{}
	err := (&yvluService{}).describeMedia(t.Context(), inv, msg, &item)
	if err == nil || !strings.Contains(err.Error(), "TGS") || item.Media != nil {
		t.Fatal(err, item)
	}
}
