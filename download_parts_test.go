package main

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"filippo.io/age"

	"backupper/internal/model"
)

func writeTestParts(t *testing.T, data []byte) (string, model.RunFile) {
	t.Helper()
	dir := t.TempDir()
	file := model.RunFile{Name: "site.tar.gz.age"}
	for off, number := 0, 1; off < len(data); number++ {
		end := min(off+97, len(data))
		name := fmt.Sprintf("%s.part%04d", file.Name, number)
		if err := os.WriteFile(filepath.Join(dir, name), data[off:end], 0o600); err != nil {
			t.Fatal(err)
		}
		file.Parts = append(file.Parts, model.Part{Name: name, Size: int64(end - off)})
		off = end
	}
	sum := sha256.Sum256(data)
	file.SHA256 = fmt.Sprintf("%x", sum)
	return dir, file
}

func TestAssembleDownloadedParts(t *testing.T) {
	data := bytes.Repeat([]byte("backup-data-"), 1000)
	dir, file := writeTestParts(t, data)
	dest := filepath.Join(t.TempDir(), "site.tar.gz")
	if err := assembleDownloadedParts(file, dir, dest, "", false); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, data) {
		t.Fatal("assembled data differs")
	}
}

func TestAssembleDownloadedPartsDecrypt(t *testing.T) {
	data := bytes.Repeat([]byte("secret-backup-"), 1000)
	recipient, err := age.NewScryptRecipient("test-password")
	if err != nil {
		t.Fatal(err)
	}
	recipient.SetWorkFactor(10)
	var encrypted bytes.Buffer
	writer, err := age.Encrypt(&encrypted, recipient)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	dir, file := writeTestParts(t, encrypted.Bytes())
	file.Encrypted = true
	dest := filepath.Join(t.TempDir(), "site.tar.gz")
	if err := assembleDownloadedParts(file, dir, dest, "test-password", true); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, data) {
		t.Fatal("decrypted data differs")
	}
}

func TestAssembleDownloadedPartsHashMismatch(t *testing.T) {
	dir, file := writeTestParts(t, []byte("broken download"))
	file.SHA256 = fmt.Sprintf("%064x", 0)
	dest := filepath.Join(t.TempDir(), "site.tar.gz")
	if err := assembleDownloadedParts(file, dir, dest, "", false); err == nil {
		t.Fatal("expected hash mismatch")
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatalf("destination must not exist after a hash mismatch: %v", err)
	}
}
