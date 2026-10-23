package site

// Integration test against real PHP, MariaDB and an FTP/SFTP server (SFTPGo).
// Run with:  BACKUPPER_IT=D:\OSPanel\modules  go test ./internal/site -run IT -v
// The database runs with --no-defaults in a temporary folder, so no installed
// MariaDB configuration or data is ever read.

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/klauspost/compress/gzip"

	"backupper/internal/model"
)

func freePort(t *testing.T) int {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func waitPort(t *testing.T, port int) {
	for i := 0; i < 100; i++ {
		c, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err == nil {
			c.Close()
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("port %d did not open", port)
}

func startBG(t *testing.T, name string, args ...string) {
	cmd := exec.Command(name, args...)
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
}

type env struct {
	mods    string
	mysql   string
	dbPort  int
	siteDir string
}

func (e env) sql(t *testing.T, db, q string) string {
	args := []string{"--no-defaults", "-h127.0.0.1", fmt.Sprintf("-P%d", e.dbPort), "-uroot", "-N", "-B"}
	if db != "" {
		args = append(args, db)
	}
	cmd := exec.Command(e.mysql, args...)
	cmd.Stdin = strings.NewReader(q)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("sql: %v\n%s", err, out)
	}
	return strings.TrimSpace(string(out))
}

func selfSigned(t *testing.T, dir string) (string, string) {
	k, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "localhost"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), DNSNames: []string{"localhost"}}
	der, _ := x509.CreateCertificate(rand.Reader, tpl, tpl, &k.PublicKey, k)
	kb, _ := x509.MarshalECPrivateKey(k)
	cert, key := filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	os.WriteFile(cert, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600)
	os.WriteFile(key, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kb}), 0o600)
	return cert, key
}

func setupEnv(t *testing.T) env {
	mods := os.Getenv("BACKUPPER_IT")
	if mods == "" {
		t.Skip("set BACKUPPER_IT to the OSPanel modules folder")
	}
	e := env{mods: mods, mysql: filepath.Join(mods, "MariaDB-10.6", "bin", "mysql.exe")}
	data := t.TempDir()
	install := exec.Command(filepath.Join(mods, "MariaDB-10.6", "bin", "mysql_install_db.exe"), "--datadir="+data)
	if out, err := install.CombinedOutput(); err != nil {
		t.Fatalf("mysql_install_db: %v\n%s", err, out)
	}
	e.dbPort = freePort(t)
	startBG(t, filepath.Join(mods, "MariaDB-10.6", "bin", "mysqld.exe"), "--no-defaults", "--datadir="+data, "--innodb-data-home-dir="+data, "--innodb-log-group-home-dir="+data, fmt.Sprintf("--port=%d", e.dbPort), "--bind-address=127.0.0.1", "--skip-name-resolve")
	waitPort(t, e.dbPort)

	e.sql(t, "", "CREATE DATABASE shop CHARACTER SET utf8mb4;")
	var b strings.Builder
	b.WriteString("CREATE TABLE goods (id INT AUTO_INCREMENT PRIMARY KEY, name VARCHAR(200), price DECIMAL(10,2), note TEXT NULL) ENGINE=InnoDB;\n")
	b.WriteString("CREATE TABLE logs (msg VARCHAR(100), at DATETIME) ENGINE=InnoDB;\n")
	b.WriteString("CREATE TABLE files (code VARCHAR(20) PRIMARY KEY, data LONGBLOB) ENGINE=InnoDB;\n")
	for i := 1; i <= 3000; i++ {
		fmt.Fprintf(&b, "INSERT INTO goods (name, price, note) VALUES ('Товар №%d «кавычки» \\'и\\' \\\\ слэш 😀', %d.%02d, %s);\n", i, i, i%100, map[bool]string{true: "NULL", false: "'описание'"}[i%7 == 0])
	}
	for i := 0; i < 700; i++ {
		fmt.Fprintf(&b, "INSERT INTO logs VALUES ('event %d', '2026-01-01 10:00:00');\n", i)
	}
	for i := 0; i < 20; i++ {
		fmt.Fprintf(&b, "INSERT INTO files VALUES ('f%02d', REPEAT(UNHEX(SHA2(RAND(), 512)), 16));\n", i)
	}
	b.WriteString("CREATE VIEW cheap AS SELECT id, name FROM goods WHERE price < 10;\n")
	e.sql(t, "shop", b.String())

	e.siteDir = t.TempDir()
	w := func(rel string, data []byte) {
		p := filepath.Join(e.siteDir, filepath.FromSlash(rel))
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	w("bitrix/.settings.php", []byte(fmt.Sprintf("<?php\nreturn ['connections' => ['value' => ['default' => ['className' => '\\\\Bitrix\\\\Main\\\\DB\\\\MysqliConnection', 'host' => '127.0.0.1:%d', 'database' => 'shop', 'login' => 'root', 'password' => '', 'options' => 2]], 'readonly' => true]];\n", e.dbPort)))
	w("index.php", []byte("<?php echo 'site';"))
	w("bitrix/cache/huge.cache", bytes.Repeat([]byte("x"), 1<<20))
	w("upload/iblock/abc/фото товара.jpg", randBytes(3<<20+123))
	w("upload/"+strings.Repeat("очень-длинное-имя-папки/", 6)+"file.txt", []byte("long path"))
	w("local/templates/main/style.css", []byte("body{}"))
	w("error.log", []byte("noise"))
	os.MkdirAll(filepath.Join(e.siteDir, "upload", "empty"), 0o755)
	return e
}

func randBytes(n int) []byte {
	b := make([]byte, n)
	rand.Read(b)
	return b
}

func startPHP(t *testing.T, e env, version string) int {
	dir := filepath.Join(e.mods, "PHP-"+version)
	port := freePort(t)
	startBG(t, filepath.Join(dir, "php.exe"), "-n", "-d", "extension_dir="+filepath.Join(dir, "ext"), "-d", "extension=pdo_mysql", "-d", "extension=openssl",
		"-d", "memory_limit=64M", "-S", fmt.Sprintf("127.0.0.1:%d", port), "-t", e.siteDir)
	waitPort(t, port)
	return port
}

func startFTP(t *testing.T, e env) (ftpPort, sftpPort int) {
	ftpPort, sftpPort = freePort(t), freePort(t)
	cfg := t.TempDir()
	cert, key := selfSigned(t, cfg)
	startBG(t, filepath.Join(e.mods, "SFTPGo", "sftpgo.exe"), "portable", "-c", cfg, "-d", e.siteDir, "-u", "u", "-p", "p", "-g", "*",
		"--ftpd-port", fmt.Sprint(ftpPort), "--ftpd-cert", cert, "--ftpd-key", key, "-s", fmt.Sprint(sftpPort), "-l", "")
	waitPort(t, ftpPort)
	waitPort(t, sftpPort)
	return
}

func TestITSiteBackup(t *testing.T) {
	e := setupEnv(t)
	ftpPort, sftpPort := startFTP(t, e)
	cases := []struct {
		php, proto string
		port       int
	}{
		{"7.4", model.ProtoFTP, ftpPort},
		{"8.3", model.ProtoFTPS, ftpPort},
		{"8.3", model.ProtoSFTP, sftpPort},
	}
	for _, c := range cases {
		t.Run(c.proto+"_php"+c.php, func(t *testing.T) {
			httpPort := startPHP(t, e, c.php)
			src := model.SiteSource{
				Protocol: c.proto, Host: "127.0.0.1", Port: c.port, User: "u", Password: "p", Root: "/",
				URL: fmt.Sprintf("http://127.0.0.1:%d", httpPort), DB: true, Files: true, CMS: "auto", CMSExcludes: true, Insecure: true,
			}
			var logs []string
			logf := func(level, f string, a ...any) { logs = append(logs, level+": "+fmt.Sprintf(f, a...)) }
			ctx := context.Background()
			s, err := Open(ctx, src, nil, logf)
			if err != nil {
				t.Fatal(err)
			}
			if s.Info.CMS != "bitrix" || s.Info.DB == nil || s.Info.DB.Tables != 4 {
				t.Fatalf("info: %+v db=%+v", s.Info, s.Info.DB)
			}
			// Small portions to cross many request boundaries (inside files and tables).
			s.maxBytes = 256 << 10
			s.SetExcludes([]string{"*.log"})

			dump := readGz(t, s.Stream(ctx, "db", nil))
			files := readGz(t, s.Stream(ctx, "files", nil))
			s.Close()

			left, _ := filepath.Glob(filepath.Join(e.siteDir, "backupper_*"))
			if len(left) != 0 {
				t.Fatalf("script not removed: %v", left)
			}

			// Restore the dump into another database and compare.
			db := "restore_" + strings.ReplaceAll(c.proto+c.php, ".", "")
			e.sql(t, "", "CREATE DATABASE "+db+" CHARACTER SET utf8mb4;")
			e.sql(t, db, string(dump))
			for _, tbl := range []string{"goods", "logs", "files"} {
				a := e.sql(t, "shop", "CHECKSUM TABLE "+tbl+";")
				b := e.sql(t, db, "CHECKSUM TABLE "+tbl+";")
				if strings.Fields(a)[1] != strings.Fields(b)[1] {
					t.Fatalf("table %s differs after restore: %s vs %s", tbl, a, b)
				}
			}
			if n := e.sql(t, db, "SELECT COUNT(*) FROM cheap;"); n != "9" {
				t.Fatalf("view: %s", n)
			}

			names := map[string][]byte{}
			tr := tar.NewReader(bytes.NewReader(files))
			for {
				h, err := tr.Next()
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Fatal(err)
				}
				data, _ := io.ReadAll(tr)
				names[h.Name] = data
			}
			for _, want := range []string{"index.php", "upload/iblock/abc/фото товара.jpg", "upload/" + strings.Repeat("очень-длинное-имя-папки/", 6) + "file.txt", "upload/empty/", "local/templates/main/style.css"} {
				if _, ok := names[want]; !ok {
					t.Fatalf("missing %q in archive", want)
				}
			}
			orig, _ := os.ReadFile(filepath.Join(e.siteDir, "upload", "iblock", "abc", "фото товара.jpg"))
			if !bytes.Equal(orig, names["upload/iblock/abc/фото товара.jpg"]) {
				t.Fatal("big file content differs")
			}
			for n := range names {
				if strings.HasPrefix(n, "bitrix/cache/") || n == "error.log" || strings.HasPrefix(n, "backupper_") {
					t.Fatalf("%s must be excluded", n)
				}
			}
			var list []string
			for n := range names {
				list = append(list, n)
			}
			sort.Strings(list)
			t.Logf("dump %d KB, archive %d entries", len(dump)>>10, len(list))
			for _, l := range logs {
				t.Log(l)
			}
		})
	}
}

func readGz(t *testing.T, rc io.ReadCloser) []byte {
	t.Helper()
	defer rc.Close()
	var buf bytes.Buffer
	n, err := io.Copy(&buf, rc)
	if err != nil {
		t.Fatalf("stream: %v (after %d bytes)", err, n)
	}
	zr, err := gzip.NewReader(&buf)
	if err != nil {
		t.Fatal(err)
	}
	out, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	return out
}
