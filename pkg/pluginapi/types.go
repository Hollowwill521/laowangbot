// Package pluginapi defines the host API for independent laowangbot plugins.
package pluginapi

import (
	"context"
	"encoding/json"
)

const Version = 1

// IDs use Telegram's marked decimal representation, keeping 64-bit precision.
type Entity struct {
	Premium   bool   `json:"premium"`
	Contact   bool   `json:"contact"`
	ID        string `json:"id"`
	Name      string `json:"name"`
	Username  string `json:"username,omitempty"`
	Bot       bool   `json:"bot"`
	Broadcast bool   `json:"broadcast"`
	Group     bool   `json:"group"`
	Self      bool   `json:"self"`
}
type Button struct {
	Text   string `json:"text"`
	Kind   string `json:"kind"`
	Data   []byte `json:"data,omitempty"`
	URL    string `json:"url,omitempty"`
	Row    int    `json:"row"`
	Column int    `json:"column"`
}
type Message struct {
	ID               int      `json:"id"`
	ChatID           string   `json:"chat_id"`
	SenderID         string   `json:"sender_id"`
	Text             string   `json:"text"`
	Date             int      `json:"date"`
	Out              bool     `json:"out"`
	Edited           bool     `json:"edited"`
	ReplyToID        int      `json:"reply_to_id"`
	Buttons          []Button `json:"buttons,omitempty"`
	URLs             []string `json:"urls,omitempty"`
	HasMedia         bool     `json:"has_media"`
	MimeType         string   `json:"mime_type,omitempty"`
	Filename         string   `json:"filename,omitempty"`
	ForwardID        string   `json:"forward_id,omitempty"`
	ForwardMessageID int      `json:"forward_message_id,omitempty"`
	ForwardDate      int      `json:"forward_date,omitempty"`
}
type Event struct {
	ChatType     string   `json:"chat_type"`
	ChannelDM    bool     `json:"channel_dm"`
	SenderIsUser bool     `json:"sender_is_user"`
	SenderPeerID string   `json:"sender_peer_id"`
	Type         string   `json:"type"`
	ChatID       string   `json:"chat_id"`
	MessageID    int      `json:"message_id"`
	SenderID     int64    `json:"sender_id"`
	Text         string   `json:"text"`
	Edited       bool     `json:"edited"`
	ReplyToID    int      `json:"reply_to_id"`
	Out          bool     `json:"out"`
	Date         int      `json:"date"`
	SelfID       string   `json:"self_id"`
	Message      *Message `json:"message,omitempty"`
}
type Request struct {
	Version int             `json:"version"`
	Type    string          `json:"type"`
	Command string          `json:"command,omitempty"`
	Args    []string        `json:"args,omitempty"`
	Text    string          `json:"text,omitempty"`
	Event   json.RawMessage `json:"event,omitempty"`
}
type Outgoing struct {
	ChatID string `json:"chat_id"`
	Text   string `json:"text"`
}
type Response struct {
	HTML     bool       `json:"html,omitempty"`
	Version  int        `json:"version"`
	Text     string     `json:"text,omitempty"`
	Error    string     `json:"error,omitempty"`
	Messages []Outgoing `json:"messages,omitempty"`
}
type Call struct {
	OffsetID   int    `json:"offset_id,omitempty"`
	Action     string `json:"action,omitempty"`
	FolderID   int    `json:"folder_id,omitempty"`
	Filename   string `json:"filename,omitempty"`
	Bytes      []byte `json:"bytes,omitempty"`
	MimeType   string `json:"mime_type,omitempty"`
	Method     string `json:"method"`
	Target     string `json:"target,omitempty"`
	User       string `json:"user,omitempty"`
	IDs        []int  `json:"ids,omitempty"`
	Limit      int    `json:"limit,omitempty"`
	MessageID  int    `json:"message_id,omitempty"`
	Text       string `json:"text,omitempty"`
	HTML       bool   `json:"html,omitempty"`
	ReplyTo    int    `json:"reply_to,omitempty"`
	Row        int    `json:"row,omitempty"`
	Column     int    `json:"column,omitempty"`
	URL        string `json:"url,omitempty"`
	Data       string `json:"data,omitempty"`
	ButtonText string `json:"button_text,omitempty"`
	Simple     bool   `json:"simple,omitempty"`
}
type Folder struct {
	ID              int    `json:"id"`
	Title           string `json:"title"`
	Contacts        bool   `json:"contacts"`
	NonContacts     bool   `json:"non_contacts"`
	Groups          bool   `json:"groups"`
	Broadcasts      bool   `json:"broadcasts"`
	Bots            bool   `json:"bots"`
	ExcludeMuted    bool   `json:"exclude_muted"`
	ExcludeArchived bool   `json:"exclude_archived"`
	IncludeCount    int    `json:"include_count"`
	ExcludeCount    int    `json:"exclude_count"`
}
type Result struct {
	Folders     []Folder  `json:"folders,omitempty"`
	CommonChats int       `json:"common_chats"`
	Entity      *Entity   `json:"entity,omitempty"`
	Messages    []Message `json:"messages,omitempty"`
	MessageID   int       `json:"message_id,omitempty"`
	Identity    string    `json:"identity,omitempty"`
	Bytes       []byte    `json:"bytes,omitempty"`
	MimeType    string    `json:"mime_type,omitempty"`
	URL         string    `json:"url,omitempty"`
	Text        string    `json:"text,omitempty"`
	Error       string    `json:"error,omitempty"`
}
type Host interface {
	Call(context.Context, Call) (Result, error)
}
