// Package cms knows popular CMS layouts: how to recognise a site root and which
// of its folders are disposable — caches, temporary files and the CMS's own
// backups, which are already compressed and often take most of the space.
// The same presets serve server folders (tar) and sites on hosting (script.php).
package cms

import (
	"os"
	"path"
	"path/filepath"
	"strings"

	"backupper/internal/model"
)

type Preset struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Markers  []string `json:"markers"`  // any of these files marks the site root
	Excludes []string `json:"excludes"` // folders relative to the site root; * and ? are wildcards
}

// Presets keep only what the site rebuilds by itself or what is a backup.
// User uploads, logs of orders and such stay in the archive.
var Presets = []Preset{
	{ID: "bitrix", Name: "1С-Битрикс",
		Markers: []string{"bitrix/.settings.php", "bitrix/php_interface/dbconn.php"},
		Excludes: []string{"bitrix/backup", "bitrix/cache", "bitrix/managed_cache", "bitrix/stack_cache", "bitrix/html_pages",
			"bitrix/tmp", "bitrix/updates", "upload/tmp", "upload/resize_cache"}},
	{ID: "wordpress", Name: "WordPress",
		Markers: []string{"wp-config.php", "wp-includes/version.php"},
		Excludes: []string{"wp-content/cache", "wp-content/et-cache", "wp-content/upgrade", "wp-content/upgrade-temp-backup",
			"wp-content/updraft", "wp-content/ai1wm-backups", "wp-content/backups-dup-lite", "wp-content/backups-dup-pro",
			"wp-content/backup-db", "wp-content/wpvividbackups", "wp-content/uploads/backwpup*"}},
	{ID: "joomla", Name: "Joomla",
		Markers: []string{"administrator/manifests/files/joomla.xml"},
		Excludes: []string{"cache", "tmp", "administrator/cache", "administrator/logs",
			"administrator/components/com_akeeba/backup", "administrator/components/com_akeebabackup/backup"}},
	{ID: "drupal", Name: "Drupal",
		Markers:  []string{"core/lib/Drupal.php", "includes/bootstrap.inc"},
		Excludes: []string{"sites/*/files/css", "sites/*/files/js", "sites/*/files/php", "sites/*/files/styles", "sites/*/files/backup_migrate"}},
	{ID: "opencart", Name: "OpenCart",
		Markers:  []string{"catalog/controller/common/home.php"},
		Excludes: []string{"image/cache", "system/storage/cache", "system/storage/logs", "system/storage/session", "system/cache", "system/logs"}},
	{ID: "modx", Name: "MODX",
		Markers:  []string{"core/model/modx/modx.class.php", "core/src/Revolution/modX.php"},
		Excludes: []string{"core/cache"}},
	{ID: "laravel", Name: "Laravel",
		Markers:  []string{"artisan"},
		Excludes: []string{"storage/framework/cache", "storage/framework/sessions", "storage/framework/views", "storage/logs", "node_modules"}},
}

// ByID returns a preset or nil.
func ByID(id string) *Preset {
	for i := range Presets {
		if Presets[i].ID == id {
			return &Presets[i]
		}
	}
	return nil
}

// Detect returns the preset whose marker files exist in dir, or nil.
func Detect(dir string) *Preset {
	for i, p := range Presets {
		for _, m := range p.Markers {
			if st, err := os.Stat(filepath.Join(dir, filepath.FromSlash(m))); err == nil && !st.IsDir() {
				return &Presets[i]
			}
		}
	}
	return nil
}

// MaxDepth is how deep below each backed up folder site roots are looked for:
// /home/bitrix → www, /var/www → site.ru/public_html, ext_www/site.ru …
const MaxDepth = 4

// maxDirs bounds the search on servers with huge trees.
const maxDirs = 20000

// skipDirs never hold a site root but may be large.
var skipDirs = map[string]bool{"node_modules": true, "vendor": true, "proc": true, "sys": true}

// Find walks the folders (absolute, slash-separated) and returns the site roots
// it recognises. A found root is not searched further.
func Find(roots []string) []model.CMSSite {
	var out []model.CMSSite
	visited := 0
	var walk func(dir string, depth int)
	walk = func(dir string, depth int) {
		if visited >= maxDirs {
			return
		}
		visited++
		if p := Detect(dir); p != nil {
			out = append(out, model.CMSSite{Path: filepath.ToSlash(dir), CMS: p.ID, Name: p.Name})
			return
		}
		if depth >= MaxDepth {
			return
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, e := range entries {
			// Symlinks are not followed: tar stores them as links.
			if !e.IsDir() || strings.HasPrefix(e.Name(), ".") || skipDirs[e.Name()] {
				continue
			}
			walk(filepath.Join(dir, e.Name()), depth+1)
		}
	}
	for _, r := range roots {
		r = strings.TrimSpace(r)
		if r == "" {
			continue
		}
		if st, err := os.Stat(r); err == nil && st.IsDir() {
			walk(filepath.Clean(r), 0)
		}
	}
	return out
}

// TarExcludes turns found sites into GNU tar --exclude patterns for members
// archived with "-C /" (no leading slash). Folder contents are skipped, the
// empty folder itself is kept so the restored site has it with its permissions.
func TarExcludes(sites []model.CMSSite) []string {
	var out []string
	for _, s := range sites {
		p := ByID(s.CMS)
		if p == nil {
			continue
		}
		root := escapeGlob(strings.TrimPrefix(path.Clean(filepath.ToSlash(s.Path)), "/"))
		for _, ex := range p.Excludes {
			out = append(out, root+"/"+ex+"/*")
		}
	}
	return out
}

// escapeGlob protects wildcard characters in a literal path.
func escapeGlob(s string) string {
	var b strings.Builder
	for _, r := range s {
		if strings.ContainsRune(`*?[]\`, r) {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}
