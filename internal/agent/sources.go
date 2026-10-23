package agent

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"backupper/internal/model"
)

// stream is one dump/archive command whose stdout becomes one file in the cloud.
type stream struct {
	file  string // file name without compression/encryption suffix, e.g. "shop.sql"
	label string
	build func(ctx context.Context) (*exec.Cmd, func(), error)
	// warnExit lists exit codes meaning "finished with warnings" (tar: 1).
	warnExit map[int]bool
}

var slugRe = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

var translit = map[rune]string{
	'а': "a", 'б': "b", 'в': "v", 'г': "g", 'д': "d", 'е': "e", 'ё': "e", 'ж': "zh", 'з': "z", 'и': "i",
	'й': "y", 'к': "k", 'л': "l", 'м': "m", 'н': "n", 'о': "o", 'п': "p", 'р': "r", 'с': "s", 'т': "t",
	'у': "u", 'ф': "f", 'х': "h", 'ц': "c", 'ч': "ch", 'ш': "sh", 'щ': "sch", 'ъ': "", 'ы': "y", 'ь': "",
	'э': "e", 'ю': "yu", 'я': "ya",
}

// Slug makes a name safe for cloud paths (Cyrillic is transliterated).
func Slug(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		if t, ok := translit[r]; ok {
			b.WriteString(t)
		} else {
			b.WriteRune(r)
		}
	}
	out := strings.Trim(slugRe.ReplaceAllString(b.String(), "_"), "_.-")
	if out == "" {
		out = "backup"
	}
	if len(out) > 60 {
		out = out[:60]
	}
	return out
}

func isRoot() bool { return os.Geteuid() == 0 }

// asPostgres returns the prefix that runs a command as the postgres OS user.
func asPostgres() []string {
	if isRoot() {
		if _, err := exec.LookPath("runuser"); err == nil {
			return []string{"runuser", "-u", "postgres", "--"}
		}
	}
	return []string{"sudo", "-n", "-u", "postgres"}
}

func command(ctx context.Context, argv []string, env []string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Env = append(os.Environ(), env...)
	cmd.Dir = os.TempDir()
	return cmd
}

func mycnfEscape(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
}

// mysqlBase returns connection args. Credentials go to a temporary
// defaults file so the password never shows up in `ps`.
func mysqlBase(src model.Source) ([]string, func(), error) {
	noop := func() {}
	if src.User == "" {
		var args []string
		if src.Host != "" {
			args = append(args, "--host="+src.Host)
		}
		if src.Port > 0 {
			args = append(args, "--port="+strconv.Itoa(src.Port))
		}
		return args, noop, nil
	}
	f, err := os.CreateTemp("", "backupper-my-*.cnf")
	if err != nil {
		return nil, noop, err
	}
	var b bytes.Buffer
	b.WriteString("[client]\n")
	b.WriteString("user=" + mycnfEscape(src.User) + "\n")
	b.WriteString("password=" + mycnfEscape(src.Password) + "\n")
	if src.Host != "" {
		b.WriteString("host=" + src.Host + "\n")
	}
	if src.Port > 0 {
		b.WriteString("port=" + strconv.Itoa(src.Port) + "\n")
	}
	_ = f.Chmod(0o600)
	_, err = f.Write(b.Bytes())
	f.Close()
	cleanup := func() { os.Remove(f.Name()) }
	if err != nil {
		cleanup()
		return nil, noop, err
	}
	return []string{"--defaults-extra-file=" + f.Name()}, cleanup, nil
}

var mysqlSystemDBs = map[string]bool{"information_schema": true, "performance_schema": true, "sys": true, "mysql": true}
var pgSkipDBs = map[string]bool{"postgres": true}

func runOutput(ctx context.Context, argv []string, env []string) (string, error) {
	cmd := command(ctx, argv, env)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("%s: %s", argv[0], msg)
	}
	return out.String(), nil
}

func ListMySQL(ctx context.Context, src model.Source) ([]string, error) {
	base, cleanup, err := mysqlBase(src)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	out, err := runOutput(ctx, append(append([]string{"mysql"}, base...), "-N", "-B", "-e", "SHOW DATABASES"), nil)
	if err != nil {
		return nil, err
	}
	var dbs []string
	for _, l := range strings.Split(out, "\n") {
		l = strings.TrimSpace(l)
		if l != "" && !mysqlSystemDBs[l] {
			dbs = append(dbs, l)
		}
	}
	return dbs, nil
}

func pgConn(src model.Source) (prefix, args, env []string) {
	if src.User == "" {
		return asPostgres(), nil, nil
	}
	host := src.Host
	if host == "" {
		host = "localhost"
	}
	args = []string{"-h", host, "-U", src.User}
	if src.Port > 0 {
		args = append(args, "-p", strconv.Itoa(src.Port))
	}
	return nil, args, []string{"PGPASSWORD=" + src.Password}
}

func ListPostgres(ctx context.Context, src model.Source) ([]string, error) {
	prefix, args, env := pgConn(src)
	argv := append(append(append([]string{}, prefix...), "psql"), args...)
	argv = append(argv, "-At", "-d", "postgres", "-c", "SELECT datname FROM pg_database WHERE NOT datistemplate AND datallowconn ORDER BY 1")
	out, err := runOutput(ctx, argv, env)
	if err != nil {
		return nil, err
	}
	var dbs []string
	for _, l := range strings.Split(out, "\n") {
		l = strings.TrimSpace(l)
		if l != "" && !pgSkipDBs[l] {
			dbs = append(dbs, l)
		}
	}
	return dbs, nil
}

func defaultName(src model.Source) string {
	if src.Name != "" {
		return src.Name
	}
	switch src.Type {
	case model.SourceMySQL:
		return "mysql"
	case model.SourcePostgres:
		return "postgres"
	case model.SourceConfigs:
		return "configs"
	}
	return "files"
}

// planStreams expands a source into the commands that dump it.
func planStreams(ctx context.Context, src model.Source, logf func(level, format string, a ...any)) ([]stream, error) {
	name := Slug(defaultName(src))
	extra := strings.Fields(src.ExtraArgs)
	switch src.Type {
	case model.SourceMySQL:
		dbs := src.Databases
		if len(dbs) == 0 && src.PerDatabase {
			list, err := ListMySQL(ctx, src)
			if err != nil {
				return nil, err
			}
			dbs = list
			logf("info", "%s: найдено баз: %d (%s)", defaultName(src), len(dbs), strings.Join(dbs, ", "))
		}
		mk := func(file, label string, target []string) stream {
			return stream{file: file, label: label, build: func(ctx context.Context) (*exec.Cmd, func(), error) {
				base, cleanup, err := mysqlBase(src)
				if err != nil {
					return nil, nil, err
				}
				argv := append([]string{"mysqldump"}, base...)
				argv = append(argv, "--single-transaction", "--quick", "--routines", "--triggers", "--hex-blob", "--no-tablespaces", "--default-character-set=utf8mb4")
				argv = append(argv, extra...)
				argv = append(argv, target...)
				return command(ctx, argv, nil), cleanup, nil
			}}
		}
		if len(dbs) == 0 {
			return []stream{mk(name+".sql", "MySQL: все базы", []string{"--all-databases"})}, nil
		}
		if src.PerDatabase {
			var out []stream
			for _, db := range dbs {
				out = append(out, mk(name+"__"+Slug(db)+".sql", "MySQL: "+db, []string{"--databases", db}))
			}
			return out, nil
		}
		return []stream{mk(name+".sql", "MySQL: "+strings.Join(dbs, ", "), append([]string{"--databases"}, dbs...))}, nil

	case model.SourcePostgres:
		dbs := src.Databases
		if len(dbs) == 0 && src.PerDatabase {
			list, err := ListPostgres(ctx, src)
			if err != nil {
				return nil, err
			}
			dbs = list
			logf("info", "%s: найдено баз: %d (%s)", defaultName(src), len(dbs), strings.Join(dbs, ", "))
		}
		if len(dbs) == 0 {
			return []stream{{file: name + ".sql", label: "PostgreSQL: все базы (pg_dumpall)", build: func(ctx context.Context) (*exec.Cmd, func(), error) {
				prefix, args, env := pgConn(src)
				argv := append(append(append([]string{}, prefix...), "pg_dumpall"), args...)
				return command(ctx, append(argv, extra...), env), func() {}, nil
			}}}, nil
		}
		var out []stream
		for _, db := range dbs {
			db := db
			file := name + "__" + Slug(db) + ".sql"
			if len(dbs) == 1 {
				file = name + ".sql"
			}
			out = append(out, stream{file: file, label: "PostgreSQL: " + db, build: func(ctx context.Context) (*exec.Cmd, func(), error) {
				prefix, args, env := pgConn(src)
				argv := append(append(append([]string{}, prefix...), "pg_dump"), args...)
				argv = append(argv, extra...)
				argv = append(argv, "--dbname="+db)
				return command(ctx, argv, env), func() {}, nil
			}})
		}
		return out, nil

	case model.SourceFiles, model.SourceConfigs:
		var rel []string
		for _, p := range src.Paths {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			if !filepath.IsAbs(p) && !strings.HasPrefix(p, "/") {
				logf("warn", "путь %q не абсолютный — пропущен", p)
				continue
			}
			if _, err := os.Lstat(p); err != nil {
				if src.Type == model.SourceFiles {
					logf("warn", "путь %s не найден — пропущен", p)
				}
				continue
			}
			rel = append(rel, strings.TrimPrefix(filepath.ToSlash(filepath.Clean(p)), "/"))
		}
		if len(rel) == 0 {
			return nil, fmt.Errorf("ни один из указанных путей не существует на сервере")
		}
		if src.Type == model.SourceConfigs {
			logf("info", "конфиги: найдено %d путей: /%s", len(rel), strings.Join(rel, ", /"))
		}
		argv := []string{"tar", "--create", "--file=-", "--ignore-failed-read", "--warning=no-file-changed", "--warning=no-file-removed"}
		for _, ex := range src.Excludes {
			ex = strings.TrimSpace(ex)
			if ex == "" {
				continue
			}
			argv = append(argv, "--exclude="+strings.TrimPrefix(ex, "/"))
		}
		argv = append(argv, extra...)
		argv = append(argv, "-C", "/")
		argv = append(argv, rel...)
		label := "Файлы: /" + strings.Join(rel, ", /")
		if src.Type == model.SourceConfigs {
			label = "Конфиги сервера"
		}
		return []stream{{file: name + ".tar", label: label, warnExit: map[int]bool{1: true}, build: func(ctx context.Context) (*exec.Cmd, func(), error) {
			return command(ctx, argv, nil), func() {}, nil
		}}}, nil
	}
	return nil, fmt.Errorf("неизвестный тип источника %q", src.Type)
}

// Probe checks a source before saving: lists databases or existing paths.
func Probe(ctx context.Context, src model.Source) model.ProbeResult {
	var res model.ProbeResult
	var err error
	switch src.Type {
	case model.SourceMySQL:
		res.Databases, err = ListMySQL(ctx, src)
	case model.SourcePostgres:
		res.Databases, err = ListPostgres(ctx, src)
	case model.SourceFiles, model.SourceConfigs:
		for _, p := range src.Paths {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			if _, e := os.Lstat(p); e == nil {
				res.Paths = append(res.Paths, p)
			}
		}
		if len(res.Paths) == 0 {
			err = fmt.Errorf("ни один из путей не найден")
		}
	default:
		err = fmt.Errorf("неизвестный тип источника")
	}
	if err != nil {
		res.Error = err.Error()
		return res
	}
	res.OK = true
	return res
}
