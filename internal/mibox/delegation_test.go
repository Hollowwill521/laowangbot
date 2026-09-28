package mibox

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestDelegationSQLite(t *testing.T) {
	raw, e := os.ReadFile("testdata/sure.db")
	if e != nil {
		t.Fatal(e)
	}
	out, e := Delegation(raw)
	if e != nil {
		t.Fatal(e)
	}
	var d struct {
		Users []struct {
			ID string `json:"id"`
		}
		Chats []struct {
			ID string `json:"id"`
		}
		Messages []struct {
			ID       int    `json:"id"`
			Redirect string `json:"redirect"`
		}
		NextID int `json:"next_id"`
	}
	if e = json.Unmarshal(out, &d); e != nil {
		t.Fatal(e)
	}
	if len(d.Users) != 1 || d.Users[0].ID != "1234567890123" || d.Chats[0].ID != "-10012345" || d.Messages[0].ID != 7 || d.Messages[0].Redirect != "ping" || d.NextID != 8 {
		t.Fatal(string(out))
	}
	if strings.Contains(string(out), "null") {
		t.Fatal(string(out))
	}
}
