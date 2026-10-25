// Package telegramtest is a fake Bot API server in --local mode for tests:
// documents, albums, edits, pins, deletions with the 48-hour rule and getFile
// that writes files to a local folder.
package telegramtest

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	Token  = "123:fake-token"
	Chat   = "-1001234567890"
	BotID  = 123
	maxAge = 48 * time.Hour
)

type Message struct {
	ID      int
	Date    time.Time
	Text    string
	Caption string
	FileID  string
	Name    string
	Group   string // media_group_id
	Pinned  bool
	Silent  bool
}

type Fake struct {
	*httptest.Server
	Dir string // where getFile puts downloads

	mu    sync.Mutex
	msgs  map[int]*Message
	files map[string][]byte
	names map[string]string
	next  int
	calls map[string]int

	// Flood answers 429 to this many next calls.
	Flood int
	// NoOldDelete refuses to delete messages older than 48 hours; with
	// SilentOldDelete it answers ok but keeps them (to test the double check).
	NoOldDelete     bool
	SilentOldDelete bool
	// Rights of the bot in the chat.
	CanDelete, CanEdit, CanPost bool
}

func New(t testing.TB) *Fake {
	f := &Fake{Dir: t.TempDir(), msgs: map[int]*Message{}, files: map[string][]byte{}, names: map[string]string{}, calls: map[string]int{}, CanDelete: true, CanEdit: true, CanPost: true}
	f.Server = httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(f.Close)
	return f
}

// Messages returns a copy of the chat ordered by id.
func (f *Fake) Messages() []Message {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []Message
	for id := 1; id <= f.next; id++ {
		if m := f.msgs[id]; m != nil {
			out = append(out, *m)
		}
	}
	return out
}

func (f *Fake) Calls(method string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[method]
}

// Age moves every message the given time into the past.
func (f *Fake) Age(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, m := range f.msgs {
		m.Date = m.Date.Add(-d)
	}
}

func reply(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": v})
}

func fail(w http.ResponseWriter, code int, desc string, extra map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	res := map[string]any{"ok": false, "error_code": code, "description": desc}
	for k, v := range extra {
		res[k] = v
	}
	json.NewEncoder(w).Encode(res)
}

// Hashtags as Telegram links them: letters, digits and "_", not digits alone.
var tagRe = regexp.MustCompile(`#[\p{L}\p{N}_]+`)

func entities(s string) []map[string]any {
	var out []map[string]any
	for _, loc := range tagRe.FindAllStringIndex(s, -1) {
		tag := s[loc[0]+1 : loc[1]]
		if strings.Trim(tag, "0123456789") == "" {
			continue
		}
		// Offsets are in UTF-16 units.
		off := len(utf16(s[:loc[0]]))
		out = append(out, map[string]any{"type": "hashtag", "offset": off, "length": len(utf16(s[loc[0]:loc[1]]))})
	}
	return out
}

func utf16(s string) []uint16 {
	var out []uint16
	for _, r := range s {
		if r >= 0x10000 {
			out = append(out, 0, 0)
		} else {
			out = append(out, 0)
		}
	}
	return out
}

func (f *Fake) msgJSON(m *Message) map[string]any {
	res := map[string]any{"message_id": m.ID, "date": m.Date.Unix(), "chat": map[string]any{"id": Chat, "type": "channel"}}
	if m.Text != "" {
		res["text"] = m.Text
	}
	if m.FileID != "" {
		res["document"] = map[string]any{"file_id": m.FileID, "file_name": m.Name, "file_size": len(f.files[m.FileID])}
		res["caption"] = m.Caption
		if e := entities(m.Caption); e != nil {
			res["caption_entities"] = e
		}
	}
	if m.Group != "" {
		res["media_group_id"] = m.Group
	}
	return res
}

func (f *Fake) add(m *Message) *Message {
	f.next++
	m.ID = f.next
	m.Date = time.Now()
	f.msgs[m.ID] = m
	return m
}

func (f *Fake) handle(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/"), "/")
	if len(parts) != 2 || parts[0] != "bot"+Token {
		fail(w, 401, "Unauthorized", nil)
		return
	}
	method := parts[1]
	params := map[string]any{}
	var upload []byte
	var uploadName string
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/") {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			fail(w, 400, "Bad Request: "+err.Error(), nil)
			return
		}
		for k, v := range r.MultipartForm.Value {
			params[k] = v[0]
		}
		for _, fh := range r.MultipartForm.File {
			fd, _ := fh[0].Open()
			upload, _ = io.ReadAll(fd)
			fd.Close()
			uploadName = fh[0].Filename
		}
		defer r.MultipartForm.RemoveAll()
	} else if r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&params); err != nil && err != io.EOF {
			fail(w, 400, "Bad Request: can't parse JSON", nil)
			return
		}
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls[method]++
	if f.Flood > 0 {
		f.Flood--
		fail(w, 429, "Too Many Requests: retry after 1", map[string]any{"parameters": map[string]any{"retry_after": 1}})
		return
	}
	str := func(k string) string { v, _ := params[k].(string); return v }
	num := func(k string) int {
		switch v := params[k].(type) {
		case float64:
			return int(v)
		case string:
			n, _ := strconv.Atoi(v)
			return n
		}
		return 0
	}
	if c := params["chat_id"]; c != nil && fmt.Sprint(c) != Chat && method != "getUpdates" {
		fail(w, 400, "Bad Request: chat not found", nil)
		return
	}
	silent := params["disable_notification"] == true || params["disable_notification"] == "true"

	switch method {
	case "getMe":
		reply(w, map[string]any{"id": BotID, "is_bot": true, "username": "fake_backup_bot"})
	case "getChat":
		reply(w, map[string]any{"id": Chat, "type": "channel", "title": "Бэкапы"})
	case "getChatMember":
		reply(w, map[string]any{"status": "administrator", "can_post_messages": f.CanPost, "can_edit_messages": f.CanEdit, "can_delete_messages": f.CanDelete})
	case "getUpdates":
		reply(w, []any{map[string]any{"update_id": 1, "my_chat_member": map[string]any{"chat": map[string]any{"id": -1001234567890, "title": "Бэкапы", "type": "channel"}}}})
	case "sendMessage":
		m := f.add(&Message{Text: str("text"), Silent: silent})
		reply(w, f.msgJSON(m))
	case "sendDocument":
		m := &Message{Caption: str("caption"), Silent: silent}
		if upload != nil {
			if len(upload) > 50<<20 {
				fail(w, 413, "Request Entity Too Large", nil)
				return
			}
			m.FileID = fmt.Sprintf("file-%d", len(f.files)+1)
			f.files[m.FileID] = upload
			f.names[m.FileID] = uploadName
		} else if id := str("document"); f.files[id] != nil {
			m.FileID = id
		} else {
			fail(w, 400, "Bad Request: wrong file identifier/HTTP URL specified", nil)
			return
		}
		m.Name = f.names[m.FileID]
		f.add(m)
		reply(w, f.msgJSON(m))
	case "sendMediaGroup":
		media, _ := params["media"].([]any)
		if len(media) < 2 || len(media) > 10 {
			fail(w, 400, "Bad Request: wrong number of media in the album", nil)
			return
		}
		group := fmt.Sprintf("g%d", f.next+1)
		var out []any
		for _, it := range media {
			item, _ := it.(map[string]any)
			id, _ := item["media"].(string)
			if item["type"] != "document" || f.files[id] == nil {
				fail(w, 400, "Bad Request: wrong media in the album", nil)
				return
			}
			cap, _ := item["caption"].(string)
			m := f.add(&Message{Caption: cap, FileID: id, Name: f.names[id], Group: group, Silent: silent})
			out = append(out, f.msgJSON(m))
		}
		reply(w, out)
	case "editMessageText":
		m := f.msgs[num("message_id")]
		switch {
		case m == nil:
			fail(w, 400, "Bad Request: message to edit not found", nil)
		case !f.CanEdit:
			fail(w, 400, "Bad Request: message can't be edited", nil)
		case m.Text == str("text"):
			fail(w, 400, "Bad Request: message is not modified: specified new message content and reply markup are exactly the same", nil)
		default:
			m.Text = str("text")
			reply(w, f.msgJSON(m))
		}
	case "editMessageReplyMarkup":
		if f.msgs[num("message_id")] == nil {
			fail(w, 400, "Bad Request: message to edit not found", nil)
		} else {
			fail(w, 400, "Bad Request: message is not modified: specified new message content and reply markup are exactly the same", nil)
		}
	case "pinChatMessage":
		if m := f.msgs[num("message_id")]; m != nil {
			m.Pinned = true
			reply(w, true)
		} else {
			fail(w, 400, "Bad Request: message to pin not found", nil)
		}
	case "deleteMessages":
		ids, _ := params["message_ids"].([]any)
		if len(ids) < 1 || len(ids) > 100 {
			fail(w, 400, "Bad Request: too many messages to delete", nil)
			return
		}
		for _, v := range ids {
			id := int(v.(float64))
			m := f.msgs[id]
			if m == nil {
				continue
			}
			if time.Since(m.Date) > maxAge && (f.NoOldDelete || f.SilentOldDelete || !f.CanDelete) {
				if f.SilentOldDelete {
					continue
				}
				fail(w, 400, "Bad Request: message can't be deleted", nil)
				return
			}
		}
		for _, v := range ids {
			id := int(v.(float64))
			if m := f.msgs[id]; m != nil && !(f.SilentOldDelete && time.Since(m.Date) > maxAge) {
				delete(f.msgs, id)
			}
		}
		reply(w, true)
	case "getFile":
		id := str("file_id")
		data := f.files[id]
		if data == nil {
			fail(w, 400, "Bad Request: invalid file_id", nil)
			return
		}
		// The real server uses "<dir>/<token>/documents"; ":" is not allowed in Windows names.
		p := filepath.Join(f.Dir, strings.ReplaceAll(Token, ":", "_"), "documents", id+".bin")
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, data, 0o644)
		abs, _ := filepath.Abs(p)
		// A --local server answers with an absolute path on its disk.
		reply(w, map[string]any{"file_id": id, "file_size": len(data), "file_path": filepath.ToSlash(abs)})
	default:
		fail(w, 404, "Not Found: method not found", nil)
	}
}
