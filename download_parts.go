package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"filippo.io/age"
	"github.com/wailsapp/wails/v2/pkg/runtime"

	"backupper/internal/model"
)

var downloadPartBackoff = [...]time.Duration{time.Second, 5 * time.Second, 15 * time.Second, 30 * time.Second}

type localPartsReader struct {
	paths []string
	cur   *os.File
}

func (r *localPartsReader) Read(p []byte) (int, error) {
	for {
		if r.cur == nil {
			if len(r.paths) == 0 {
				return 0, io.EOF
			}
			f, err := os.Open(r.paths[0])
			r.paths = r.paths[1:]
			if err != nil {
				return 0, err
			}
			r.cur = f
		}
		n, err := r.cur.Read(p)
		if err != io.EOF {
			return n, err
		}
		closeErr := r.cur.Close()
		r.cur = nil
		if closeErr != nil {
			return n, closeErr
		}
		if n > 0 {
			return n, nil
		}
	}
}

func (r *localPartsReader) Close() error {
	if r.cur == nil {
		return nil
	}
	err := r.cur.Close()
	r.cur = nil
	return err
}

func assembleDownloadedParts(file model.RunFile, partsDir, dest, password string, decrypt bool) (retErr error) {
	paths := make([]string, len(file.Parts))
	for i, p := range file.Parts {
		paths[i] = filepath.Join(partsDir, p.Name)
	}
	parts := &localPartsReader{paths: paths}
	defer parts.Close()
	hash := sha256.New()
	var source io.Reader = io.TeeReader(parts, hash)
	if decrypt && file.Encrypted {
		identity, err := age.NewScryptIdentity(password)
		if err != nil {
			return err
		}
		source, err = age.Decrypt(source, identity)
		if err != nil {
			return fmt.Errorf("расшифровка: %w (пароль шифрования изменился?)", err)
		}
	}
	tmp := dest + ".part"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, source)
	closeErr := out.Close()
	if copyErr != nil {
		os.Remove(tmp)
		return copyErr
	}
	if closeErr != nil {
		os.Remove(tmp)
		return closeErr
	}
	sum := hex.EncodeToString(hash.Sum(nil))
	if file.SHA256 != "" && !strings.EqualFold(sum, file.SHA256) {
		os.Remove(tmp)
		return errors.New("контрольная сумма скачанных частей не совпала — удалите скрытую папку частей и повторите")
	}
	os.Remove(dest)
	if err := os.Rename(tmp, dest); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

func (a *App) downloadPartOnce(serverID, storageID, dir, file string, part model.Part, dest string, pw *progressWriter) error {
	q := url.Values{"storage": {storageID}, "dir": {dir}, "file": {file}, "part": {part.Name}}
	resp, err := a.mgr.Stream(a.ctx, serverID, "/api/downloadpart?"+q.Encode())
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	tmp := dest + ".download"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	written, copyErr := io.Copy(io.MultiWriter(out, pw), resp.Body)
	closeErr := out.Close()
	if copyErr != nil {
		os.Remove(tmp)
		return copyErr
	}
	if closeErr != nil {
		os.Remove(tmp)
		return closeErr
	}
	if written != part.Size {
		os.Remove(tmp)
		return fmt.Errorf("часть оборвалась: получено %d из %d байт", written, part.Size)
	}
	os.Remove(dest)
	if err := os.Rename(tmp, dest); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

func (a *App) downloadPart(serverID, storageID, dir, file string, part model.Part, dest string, pw *progressWriter) error {
	if info, err := os.Stat(dest); err == nil && info.Size() == part.Size {
		pw.done += part.Size
		a.logDownload("Часть уже на ПК: part=%s bytes=%d", part.Name, part.Size)
		return nil
	}
	base := pw.done
	var lastErr error
	for attempt := 1; attempt <= len(downloadPartBackoff)+1; attempt++ {
		pw.done = base
		lastErr = a.downloadPartOnce(serverID, storageID, dir, file, part, dest, pw)
		if lastErr == nil {
			a.logDownload("Часть получена: part=%s bytes=%d attempt=%d", part.Name, part.Size, attempt)
			return nil
		}
		a.logDownload("Ошибка части: part=%s bytes=%d attempt=%d: %v", part.Name, pw.done, attempt, lastErr)
		if attempt == len(downloadPartBackoff)+1 {
			break
		}
		delay := downloadPartBackoff[attempt-1]
		a.logDownload("Повтор части %s через %s", part.Name, delay)
		select {
		case <-time.After(delay):
		case <-a.ctx.Done():
			return a.ctx.Err()
		}
	}
	return lastErr
}

func (a *App) downloadBackupFileParts(serverID, storageID, dir string, file model.RunFile, dest string, decrypt bool) (retErr error) {
	started := time.Now()
	a.logDownload("Начало по частям: server=%s storage=%s dir=%s file=%s destination=%s", serverID, storageID, dir, file.Name, dest)
	var completed int64
	defer func() {
		if retErr != nil {
			a.logDownload("Ошибка скачивания по частям: file=%s destination=%s bytes=%d duration=%s: %v", file.Name, dest, completed, time.Since(started).Round(time.Second), retErr)
		} else {
			a.logDownload("Готово по частям: file=%s destination=%s bytes=%d duration=%s", file.Name, dest, completed, time.Since(started).Round(time.Second))
		}
	}()
	partsDir := filepath.Join(filepath.Dir(dest), "."+filepath.Base(dest)+".parts")
	if err := os.MkdirAll(partsDir, 0o700); err != nil {
		return err
	}
	for _, part := range file.Parts {
		if !safeLocalName(part.Name) {
			return fmt.Errorf("неверное имя части в копии: %q", part.Name)
		}
		pw := &progressWriter{a: a, key: dir + "/" + file.Name, total: file.Size, done: completed}
		if err := a.downloadPart(serverID, storageID, dir, file.Name, part, filepath.Join(partsDir, part.Name), pw); err != nil {
			completed = pw.done
			return err
		}
		completed = pw.done
		runtime.EventsEmit(a.ctx, "download", map[string]any{"key": pw.key, "done": completed, "total": file.Size})
	}
	password := a.store.Get().Settings.EncryptionPassword
	if err := assembleDownloadedParts(file, partsDir, dest, password, decrypt); err != nil {
		return err
	}
	if err := os.RemoveAll(partsDir); err != nil {
		a.logDownload("Не удалось удалить временные части %s: %v", partsDir, err)
	}
	return nil
}
