package mibox

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// Delegation converts TeleBox/MiBox SQLite user/chat lists and sure rules.
// The host's narrower command authorization policy still applies after import.
func Delegation(raw []byte) ([]byte, error) {
	type entry struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	type rule struct {
		ID       int64  `json:"id"`
		Msg      string `json:"msg"`
		Redirect string `json:"redirect,omitempty"`
	}
	d := struct {
		Users    []entry `json:"users"`
		Chats    []entry `json:"chats"`
		Messages []rule  `json:"messages,omitempty"`
		NextID   int64   `json:"next_id,omitempty"`
	}{Users: []entry{}, Chats: []entry{}, NextID: 1}
	for _, table := range []string{"users", "chats", "msgs"} {
		rows, e := readTable(raw, table)
		if e != nil {
			if table != "users" && strings.Contains(e.Error(), "没有 "+table+" 表") {
				continue
			}
			return nil, e
		}
		for _, row := range rows {
			if len(row) < 2 {
				return nil, fmt.Errorf("invalid %s row", table)
			}
			id, ok := row[0].(int64)
			if !ok {
				return nil, fmt.Errorf("invalid %s id", table)
			}
			text, ok := row[1].(string)
			if !ok {
				return nil, fmt.Errorf("invalid %s text", table)
			}
			switch table {
			case "users":
				d.Users = append(d.Users, entry{strconv.FormatInt(id, 10), text})
			case "chats":
				d.Chats = append(d.Chats, entry{strconv.FormatInt(id, 10), text})
			case "msgs":
				r := rule{ID: id, Msg: text}
				if len(row) > 2 && row[2] != nil {
					r.Redirect, ok = row[2].(string)
					if !ok {
						return nil, fmt.Errorf("invalid redirect")
					}
				}
				d.Messages = append(d.Messages, r)
				if id >= d.NextID {
					d.NextID = id + 1
				}
			}
		}
	}
	return json.MarshalIndent(d, "", "  ")
}
