package cms

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"backupper/internal/model"
)

func touch(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("<?php"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestFind(t *testing.T) {
	home := t.TempDir()
	// The layout of BitrixVM: /home/bitrix/www plus extra sites in ext_www.
	touch(t, filepath.Join(home, "www/bitrix/.settings.php"))
	touch(t, filepath.Join(home, "www/bitrix/modules/main/include.php"))
	touch(t, filepath.Join(home, "ext_www/blog.ru/wp-config.php"))
	touch(t, filepath.Join(home, "ext_www/blog.ru/wp-content/plugins/x/wp-config.php")) // inside a found root: ignored
	touch(t, filepath.Join(home, ".cache/wp-config.php"))                               // hidden: ignored
	touch(t, filepath.Join(home, "a/b/c/d/e/wp-config.php"))                            // too deep

	got := Find([]string{home})
	var s []string
	for _, f := range got {
		rel, _ := filepath.Rel(home, filepath.FromSlash(f.Path))
		s = append(s, filepath.ToSlash(rel)+"="+f.CMS)
	}
	if strings.Join(s, ",") != "ext_www/blog.ru=wordpress,www=bitrix" {
		t.Fatalf("found %v", s)
	}
}

func TestTarExcludes(t *testing.T) {
	ex := TarExcludes([]model.CMSSite{{Path: "/home/bitrix/www", CMS: "bitrix"}, {Path: "/var/www/a[1]", CMS: "wordpress"}})
	if ex[0] != "home/bitrix/www/bitrix/backup/*" {
		t.Fatalf("first pattern %q", ex[0])
	}
	if !strings.HasPrefix(ex[len(ex)-1], `var/www/a\[1\]/wp-content/`) {
		t.Fatalf("wildcards in the path must be escaped: %q", ex[len(ex)-1])
	}
	for _, p := range Presets {
		if len(p.Markers) == 0 || len(p.Excludes) == 0 || p.Name == "" {
			t.Fatalf("preset %s is incomplete", p.ID)
		}
	}
}
