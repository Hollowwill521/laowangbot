package hostbridge

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"

	"github.com/OrionG-hub/laowangbot/internal/bot"
	"github.com/OrionG-hub/laowangbot/pkg/pluginapi"
	"github.com/gotd/td/tg"
)

// Serialize local read-modify-write operations so concurrent plugin calls do not
// overwrite each other's changes to dialog filters.
var folderMu sync.Mutex

func checked(ok bool, err error) error {
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("Telegram rejected operation")
	}
	return nil
}
func chatAction(ctx context.Context, b *bot.Client, p tg.InputPeerClass, action string, id int) error {
	switch action {
	case "mute", "unmute":
		s := tg.InputPeerNotifySettings{}
		mute := action == "mute"
		until := 0
		if mute {
			until = 2147483647
		}
		s.SetMuteUntil(until)
		s.SetShowPreviews(!mute)
		s.SetSilent(mute)
		return checked(b.API().AccountUpdateNotifySettings(ctx, &tg.AccountUpdateNotifySettingsRequest{Peer: &tg.InputNotifyPeer{Peer: p}, Settings: s}))
	case "archive", "unarchive":
		n := 0
		if action == "archive" {
			n = 1
		}
		_, err := b.API().FoldersEditPeerFolders(ctx, []tg.InputFolderPeer{{Peer: p, FolderID: n}})
		return err
	case "block":
		return checked(b.API().ContactsBlock(ctx, &tg.ContactsBlockRequest{ID: p}))
	case "report":
		return checked(b.API().AccountReportPeer(ctx, &tg.AccountReportPeerRequest{Peer: p, Reason: &tg.InputReportReasonSpam{}, Message: "spam"}))
	case "delete_history":
		for {
			v, err := b.API().MessagesDeleteHistory(ctx, &tg.MessagesDeleteHistoryRequest{Peer: p, Revoke: true, MaxID: 0})
			if err != nil {
				return err
			}
			if v.Offset == 0 {
				return nil
			}
			if err = ctx.Err(); err != nil {
				return err
			}
		}
	case "folder_add", "folder_remove", "folder_normalize":
		if id < 1 {
			return errors.New("folder_id must be positive")
		}
		if id == 1 {
			switch action {
			case "folder_add":
				return chatAction(ctx, b, p, "archive", 0)
			case "folder_remove":
				return chatAction(ctx, b, p, "unarchive", 0)
			default:
				return errors.New("archive has no filter rules")
			}
		}
		folderMu.Lock()
		defer folderMu.Unlock()
		filters, err := b.API().MessagesGetDialogFilters(ctx)
		if err != nil {
			return err
		}
		for _, v := range filters.Filters {
			f, ok := v.(*tg.DialogFilter)
			if !ok || f.ID != id {
				continue
			}
			copy := *f
			switch action {
			case "folder_normalize":
				copy.Contacts = false
				copy.NonContacts = false
				copy.Flags.Unset(0)
				copy.Flags.Unset(1)
			case "folder_add":
				key := peerKey(p, b.SelfID())
				if key == "" {
					return errors.New("unsupported folder peer")
				}
				copy.IncludePeers = withoutPeer(f.IncludePeers, key, b.SelfID())
				copy.IncludePeers = append(copy.IncludePeers, p)
				copy.ExcludePeers = withoutPeer(f.ExcludePeers, key, b.SelfID())
			case "folder_remove":
				key := peerKey(p, b.SelfID())
				if key == "" {
					return errors.New("unsupported folder peer")
				}
				copy.IncludePeers = withoutPeer(f.IncludePeers, key, b.SelfID())
				copy.PinnedPeers = withoutPeer(f.PinnedPeers, key, b.SelfID())
			}
			q := &tg.MessagesUpdateDialogFilterRequest{ID: id}
			q.SetFilter(&copy)
			return checked(b.API().MessagesUpdateDialogFilter(ctx, q))
		}
		return fmt.Errorf("custom folder %d not found", id)
	default:
		return errors.New("unknown chat action")
	}
}
func peerKey(p tg.InputPeerClass, self int64) string {
	switch v := p.(type) {
	case *tg.InputPeerSelf:
		return "user:" + strconv.FormatInt(self, 10)
	case *tg.InputPeerUser:
		return "user:" + strconv.FormatInt(v.UserID, 10)
	case *tg.InputPeerChat:
		return "chat:" + strconv.FormatInt(v.ChatID, 10)
	case *tg.InputPeerChannel:
		return "channel:" + strconv.FormatInt(v.ChannelID, 10)
	case *tg.InputPeerUserFromMessage:
		return "user:" + strconv.FormatInt(v.UserID, 10)
	case *tg.InputPeerChannelFromMessage:
		return "channel:" + strconv.FormatInt(v.ChannelID, 10)
	}
	return ""
}
func withoutPeer(in []tg.InputPeerClass, key string, self int64) []tg.InputPeerClass {
	out := make([]tg.InputPeerClass, 0, len(in))
	for _, p := range in {
		if peerKey(p, self) != key {
			out = append(out, p)
		}
	}
	return out
}
func folders(ctx context.Context, b *bot.Client) (r pluginapi.Result, err error) {
	v, err := b.API().MessagesGetDialogFilters(ctx)
	if err != nil {
		return r, err
	}
	for _, item := range v.Filters {
		if f, ok := item.(*tg.DialogFilter); ok {
			r.Folders = append(r.Folders, pluginapi.Folder{ID: f.ID, Title: f.Title.Text, Contacts: f.Contacts, NonContacts: f.NonContacts, Groups: f.Groups, Broadcasts: f.Broadcasts, Bots: f.Bots, ExcludeMuted: f.ExcludeMuted, ExcludeArchived: f.ExcludeArchived, IncludeCount: len(f.IncludePeers), ExcludeCount: len(f.ExcludePeers)})
		}
	}
	return r, nil
}
func userInfo(ctx context.Context, b *bot.Client, p tg.InputPeerClass) (r pluginapi.Result, err error) {
	u, ok := bot.InputUser(p)
	if !ok {
		return r, errors.New("user_info requires a user")
	}
	v, err := b.API().UsersGetFullUser(ctx, u)
	if err != nil {
		return r, err
	}
	targetID := entity(b, p).ID
	if strconv.FormatInt(v.FullUser.ID, 10) != targetID {
		return r, errors.New("user_info returned a different full user")
	}
	var matched *tg.User
	for _, item := range v.Users {
		if user, ok := item.(*tg.User); ok && !user.Min && strconv.FormatInt(user.ID, 10) == targetID {
			matched = user
			break
		}
	}
	if matched == nil {
		return r, errors.New("user_info returned no complete matching user")
	}
	b.Peers().RememberUsers(v.Users)
	b.Peers().RememberChats(v.Chats)
	e := entity(b, p)
	e.Premium = matched.Premium
	e.Contact = matched.Contact
	e.Bot = matched.Bot
	e.Self = matched.Self || matched.ID == b.SelfID()
	r.Entity = e
	r.CommonChats = v.FullUser.CommonChatsCount
	return r, nil
}
