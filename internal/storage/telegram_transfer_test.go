package storage

import (
	"testing"

	"backupper/internal/model"
)

func TestTelegramSiteTransferMergesAndRejectsConflicts(t *testing.T) {
	SetDataDir(t.TempDir())
	t.Cleanup(func() { SetDataDir("") })
	s := model.Storage{ID: "tg", ChatID: "chat"}
	ix := tgIndexFor(s.ID, s.ChatID)
	ix.Runs["/root/sites/other/2026-01-01_00-00-00"] = &tgRun{Done: true, Files: map[string]*tgFile{"manifest.json": {MessageID: 1}}}
	transfer := TelegramSiteIndex{Chat: "chat", RotationBlocked: true, Runs: map[string]*tgRun{
		"/root/sites/site/2026-01-01_00-00-00": {Done: true, Files: map[string]*tgFile{"manifest.json": {MessageID: 2}}},
	}}
	if err := ImportTelegramSite(s, "/root/sites/site", transfer); err != nil {
		t.Fatal(err)
	}
	if err := ImportTelegramSite(s, "/root/sites/site", transfer); err != nil {
		t.Fatal(err)
	}
	if len(ix.Runs) != 2 {
		t.Fatalf("lost unrelated copy: %#v", ix.Runs)
	}
	if ix.Probe.Result != "no" {
		t.Fatal("lost rotation block")
	}
	out, err := ExportTelegramSite(s, "/root/sites/site")
	if err != nil || len(out.Runs) != 1 {
		t.Fatalf("unexpected export: %#v, %v", out, err)
	}
	transfer.Runs["/root/sites/site/2026-01-01_00-00-00"].Files["manifest.json"].MessageID = 3
	if err := ImportTelegramSite(s, "/root/sites/site", transfer); err == nil {
		t.Fatal("accepted conflicting copy")
	}
	transfer.Chat = "other"
	if err := ImportTelegramSite(s, "/root/sites/site", transfer); err == nil {
		t.Fatal("accepted another chat")
	}
}
