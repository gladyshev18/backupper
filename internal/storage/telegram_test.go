package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"backupper/internal/model"
	"backupper/internal/storage/telegramtest"
)

func tgConfig(f *telegramtest.Fake) model.Storage {
	return model.Storage{ID: "tg", Kind: model.StorageTelegram, Token: telegramtest.Token, ChatID: telegramtest.Chat, Root: "/Backupper",
		TgServer: &model.TelegramServer{DirectURL: f.URL}}
}

func tgStorage(t *testing.T, f *telegramtest.Fake) *Telegram {
	t.Helper()
	SetDataDir(t.TempDir())
	st, err := New(tgConfig(f))
	if err != nil {
		t.Fatal(err)
	}
	return st.(*Telegram)
}

func TestHashtag(t *testing.T) {
	for in, want := range map[string]string{"re308": "#re308", "my-site.ru": "#my_site_ru", "2026": "#n2026", "сайт": "#сайт", "--": ""} {
		if got := hashtag(in); got != want {
			t.Errorf("hashtag(%q) = %q, want %q", in, got, want)
		}
	}
	for n, want := range map[int]string{1: "[1]", 10: "[10]", 11: "[6 5]", 12: "[6 6]", 21: "[7 7 7]"} {
		if g := fmt.Sprint(groups(n)); g != want {
			t.Errorf("groups(%d) = %s, want %s", n, g, want)
		}
	}
}

// putRun uploads parts of one file plus the manifest, as the runner does.
func putRun(t *testing.T, st *Telegram, dir string, parts int, status string) {
	t.Helper()
	ctx := context.Background()
	rf := model.RunFile{Name: "db.sql.gz.age", Encrypted: true}
	for i := 1; i <= parts; i++ {
		name := fmt.Sprintf("%s.part%04d", rf.Name, i)
		data := []byte(strings.Repeat(fmt.Sprintf("part %d;", i), 100))
		if err := st.Put(ctx, path.Join(dir, name), bytes.NewReader(data), int64(len(data))); err != nil {
			t.Fatal(err)
		}
		rf.Parts = append(rf.Parts, model.Part{Name: name, Size: int64(len(data))})
		rf.Size += int64(len(data))
	}
	man := model.Manifest{Version: 1, Server: "re308", JobName: "Сайт", Status: status, StartedAt: time.Now(), Files: []model.RunFile{rf}}
	mb, _ := json.Marshal(man)
	if err := st.Put(ctx, path.Join(dir, "manifest.json"), bytes.NewReader(mb), int64(len(mb))); err != nil {
		t.Fatal(err)
	}
}

func TestTelegramPutListGetDelete(t *testing.T) {
	f := telegramtest.New(t)
	st := tgStorage(t, f)
	ctx := context.Background()
	dir := "/Backupper/re308/sajt/2026-10-25_03-00-00"
	putRun(t, st, dir, 12, model.RunSuccess)

	msgs := f.Messages()
	var docs []telegramtest.Message
	albums := map[string]int{}
	for _, m := range msgs {
		if m.FileID != "" {
			docs = append(docs, m)
			if m.Group != "" {
				albums[m.Group]++
			}
		}
		if strings.Contains(m.Caption, "идёт загрузка") {
			t.Errorf("temporary message left: %q", m.Caption)
		}
	}
	if len(docs) != 13 {
		t.Fatalf("documents in chat: %d, want 12 parts + manifest", len(docs))
	}
	if len(albums) != 2 {
		t.Fatalf("albums: %v, want two albums of 6", albums)
	}
	if last := docs[len(docs)-1]; last.Name != "manifest.json" || !strings.Contains(last.Caption, "✅ Копия полная") {
		t.Fatalf("manifest must be the last document, got %q %q", last.Name, last.Caption)
	}
	if c := docs[2].Caption; !strings.Contains(c, "#re308 #sajt #d20261025") || !strings.Contains(c, "часть 3 из 12") {
		t.Fatalf("caption of part 3: %q", c)
	}
	var pinned *telegramtest.Message
	for i := range msgs {
		if msgs[i].Pinned {
			pinned = &msgs[i]
		}
	}
	if pinned == nil || !strings.Contains(pinned.Text, "Бэкапы сервера re308") || !strings.Contains(pinned.Text, "https://t.me/c/1234567890/") {
		t.Fatalf("pinned table of contents: %+v", pinned)
	}

	entries, err := st.List(ctx, "/Backupper/re308/sajt")
	if err != nil || len(entries) != 1 || !entries[0].IsDir || entries[0].Name != "2026-10-25_03-00-00" {
		t.Fatalf("list job dir: %+v %v", entries, err)
	}
	if files, _ := st.List(ctx, dir); len(files) != 13 {
		t.Fatalf("files in run: %d", len(files))
	}

	rc, err := st.Get(ctx, path.Join(dir, "db.sql.gz.age.part0007"))
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(rc)
	rc.Close()
	if want := strings.Repeat("part 7;", 100); string(got) != want {
		t.Fatalf("part 7 content: %q", got)
	}
	if left, _ := filepath.Glob(filepath.Join(f.Dir, "*", "documents", "*")); len(left) != 0 {
		t.Fatalf("downloaded files must be deleted after reading: %v", left)
	}

	rc, err = st.Get(ctx, path.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var man model.Manifest
	json.NewDecoder(rc).Decode(&man)
	rc.Close()
	if man.Telegram == nil || len(man.Telegram.Messages) != 12 || man.Telegram.Messages["db.sql.gz.age.part0001"].FileID == "" {
		t.Fatalf("manifest must carry the message index: %+v", man.Telegram)
	}

	if err := st.Delete(ctx, dir); err != nil {
		t.Fatal(err)
	}
	if _, err := st.List(ctx, "/Backupper/re308/sajt"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("after delete: %v", err)
	}
	for _, m := range f.Messages() {
		if m.FileID != "" {
			t.Fatalf("message left after delete: %+v", m)
		}
	}
}

// Purge after a failed file deletes its temporary messages one by one.
func TestTelegramDeletePart(t *testing.T) {
	f := telegramtest.New(t)
	st := tgStorage(t, f)
	p := "/Backupper/a/b/2026-10-25_03-00-00/x.part0001"
	if err := st.Put(context.Background(), p, bytes.NewReader([]byte("x")), 1); err != nil {
		t.Fatal(err)
	}
	if err := st.Delete(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	for _, m := range f.Messages() {
		if m.FileID != "" {
			t.Fatalf("part message left: %+v", m)
		}
	}
	if _, err := st.List(context.Background(), path.Dir(p)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("empty run must disappear: %v", err)
	}
}

func TestTelegramIndexSurvivesRestart(t *testing.T) {
	f := telegramtest.New(t)
	st := tgStorage(t, f)
	dir := "/Backupper/re308/sajt/2026-10-25_03-00-00"
	putRun(t, st, dir, 2, model.RunSuccess)
	// A new agent process reads the index from disk.
	tgMu.Lock()
	tgIndexes = map[string]*tgIndex{}
	tgMu.Unlock()
	st2, _ := New(tgConfig(f))
	if _, err := st2.List(context.Background(), dir); err != nil {
		t.Fatalf("index lost after restart: %v", err)
	}
}

func TestTelegramFloodWait(t *testing.T) {
	f := telegramtest.New(t)
	st := tgStorage(t, f)
	f.Flood = 1
	if err := st.Put(context.Background(), "/Backupper/a/b/2026-10-25_03-00-00/x.part0001", bytes.NewReader([]byte("hello")), 5); err != nil {
		t.Fatalf("429 must be waited out: %v", err)
	}
}

func TestTelegramErrorsHideToken(t *testing.T) {
	st := tgStorage(t, telegramtest.New(t))
	st.tr = &directTransport{url: "http://127.0.0.1:1", hc: httpClient()}
	err := st.Put(context.Background(), "/Backupper/a/b/c/x.part0001", bytes.NewReader([]byte("x")), 1)
	if err == nil || strings.Contains(err.Error(), "fake-token") {
		t.Fatalf("error must not contain the token: %v", err)
	}
}

func TestTelegramOldMessages(t *testing.T) {
	for _, silent := range []bool{false, true} {
		f := telegramtest.New(t)
		st := tgStorage(t, f)
		dir := "/Backupper/re308/sajt/2026-10-20_03-00-00"
		putRun(t, st, dir, 3, model.RunSuccess)
		f.Age(72 * time.Hour)
		st.ix.mu.Lock()
		st.ix.Runs[dir].Created = time.Now().Add(-72 * time.Hour)
		st.ix.mu.Unlock()
		f.NoOldDelete, f.SilentOldDelete = !silent, silent
		if err := st.Delete(context.Background(), dir); err == nil {
			t.Fatalf("silent=%v: deleting old messages must fail", silent)
		}
		if st.RotationBlocked() == "" || TelegramNote(tgConfig(f)) == "" {
			t.Fatalf("silent=%v: rotation must be reported as blocked", silent)
		}
		if _, err := st.List(context.Background(), dir); err != nil {
			t.Fatalf("silent=%v: a copy that was not deleted must stay in the index", silent)
		}
	}
}

func TestTelegramProbe(t *testing.T) {
	f := telegramtest.New(t)
	st := tgStorage(t, f)
	putRun(t, st, "/Backupper/re308/sajt/2026-10-25_03-00-00", 1, model.RunSuccess)
	if st.ix.Probe.MessageID == 0 {
		t.Fatal("probe message must be sent with the first part")
	}
	f.Age(50 * time.Hour)
	st.ix.Probe.Sent = time.Now().Add(-50 * time.Hour)
	putRun(t, st, "/Backupper/re308/sajt/2026-10-26_03-00-00", 1, model.RunSuccess)
	if st.ix.Probe.Result != "ok" {
		t.Fatalf("probe result %q, want ok", st.ix.Probe.Result)
	}

	f2 := telegramtest.New(t)
	st2 := tgStorage(t, f2)
	f2.NoOldDelete = true
	putRun(t, st2, "/Backupper/re308/sajt/2026-10-25_03-00-00", 1, model.RunSuccess)
	f2.Age(50 * time.Hour)
	st2.ix.Probe.Sent = time.Now().Add(-50 * time.Hour)
	putRun(t, st2, "/Backupper/re308/sajt/2026-10-26_03-00-00", 1, model.RunSuccess)
	if st2.RotationBlocked() == "" {
		t.Fatal("a probe that cannot be deleted must block rotation")
	}
}

func TestTelegramCheck(t *testing.T) {
	f := telegramtest.New(t)
	SetDataDir(t.TempDir())
	f.CanDelete = false
	res := Test(context.Background(), tgConfig(f), 1024)
	if !res.OK || res.MaxFile != TelegramMaxFile || len(res.Notes) != 1 || !strings.Contains(res.Notes[0], "Удаление сообщений") {
		t.Fatalf("test result: %+v", res)
	}
	if res.SpeedBps <= 0 {
		t.Fatalf("speed test did not run: %+v", res)
	}
	for _, m := range f.Messages() {
		if m.FileID != "" {
			t.Fatalf("speed test file left in the chat: %+v", m)
		}
	}
	f.CanPost = false
	if res := Test(context.Background(), tgConfig(f), 0); res.OK {
		t.Fatal("a bot that cannot post must fail the check")
	}
	chats, err := TelegramChats(context.Background(), model.Storage{Token: telegramtest.Token, TgServer: &model.TelegramServer{DirectURL: f.URL}})
	if err != nil || len(chats) != 1 || chats[0].ID != -1001234567890 {
		t.Fatalf("chats: %+v %v", chats, err)
	}
}
