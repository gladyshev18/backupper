package desktop

import (
	"path/filepath"
	"testing"

	"backupper/internal/model"
)

// A pointer stored by Update must not stay shared with the caller: InstallTelegramServer
// clears TgServer.Key on its copy before returning it to the UI.
func TestUpdateDoesNotAliasCaller(t *testing.T) {
	s := &Store{path: filepath.Join(t.TempDir(), "panel.dat")}
	st := model.Storage{ID: "tg", TgServer: &model.TelegramServer{Host: "h", Key: "secret"}}
	if err := s.Update(func(c *Config) error {
		c.Storages = append(c.Storages, st)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	st.TgServer.Key = ""
	if got := s.Get().Storages[0].TgServer.Key; got != "secret" {
		t.Fatalf("key in config = %q, want it kept", got)
	}
}
