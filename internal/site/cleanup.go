package site

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"backupper/internal/model"
)

// cleanupEntry contains only the credentials needed to remove this session's files.
type cleanupEntry struct {
	Source model.SiteSource `json:"source"`
	Name   string           `json:"name"`
}

// CleanupQueue persists cleanup obligations before a script is uploaded.
// Active sessions are kept in memory and cannot be touched by the retry worker.
type CleanupQueue struct {
	mu      sync.Mutex
	file    string
	entries map[string]cleanupEntry
	active  map[string]bool
	wake    chan struct{}
	dial    func(context.Context, model.SiteSource, HostKeys) (Remote, error)
}

func OpenCleanupQueue(dir string) (*CleanupQueue, error) {
	q := &CleanupQueue{file: filepath.Join(dir, "site-cleanup.json"), entries: map[string]cleanupEntry{}, active: map[string]bool{}, wake: make(chan struct{}, 1), dial: Dial}
	b, err := os.ReadFile(q.file)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if err == nil {
		if err := json.Unmarshal(b, &q.entries); err != nil {
			return nil, fmt.Errorf("чтение очереди очистки сайтов: %w", err)
		}
		if q.entries == nil {
			q.entries = map[string]cleanupEntry{}
		}
	}
	for name, e := range q.entries {
		if name != e.Name || !validScriptName(name) {
			return nil, errors.New("неверная запись очереди очистки сайтов")
		}
	}
	return q, nil
}

func validScriptName(name string) bool {
	return staleRe.MatchString(name+".php") && len(name) == len("backupper_")+16
}

// HasPending prevents moving a site away while its agent still owes cleanup.
func (q *CleanupQueue) HasPending(src model.SiteSource) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, e := range q.entries {
		old := e.Source
		oldProto, newProto := old.Protocol, src.Protocol
		if oldProto == "" {
			oldProto = model.ProtoFTP
		}
		if newProto == "" {
			newProto = model.ProtoFTP
		}
		if strings.EqualFold(old.Host, src.Host) && old.User == src.User && defaultPort(old) == defaultPort(src) && oldProto == newProto && path.Clean("/"+old.Root) == path.Clean("/"+src.Root) {
			return true
		}
	}
	return false
}

func (q *CleanupQueue) save() error {
	b, err := json.Marshal(q.entries)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(q.file), 0o700); err != nil {
		return err
	}
	tmp := q.file + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, q.file); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func (q *CleanupQueue) register(src model.SiteSource, name string) error {
	if !validScriptName(name) {
		return errors.New("неверное имя временного скрипта")
	}
	// Database credentials and other site settings are not needed by the cleaner.
	src = model.SiteSource{Protocol: src.Protocol, Host: src.Host, Port: src.Port, User: src.User, Password: src.Password, Root: src.Root, Insecure: src.Insecure}
	q.mu.Lock()
	defer q.mu.Unlock()
	if _, exists := q.entries[name]; exists {
		return errors.New("временный скрипт уже зарегистрирован")
	}
	q.entries[name] = cleanupEntry{Source: src, Name: name}
	q.active[name] = true
	if err := q.save(); err != nil {
		delete(q.entries, name)
		delete(q.active, name)
		return err
	}
	return nil
}

func (q *CleanupQueue) release(name string, cleaned bool) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	delete(q.active, name)
	if cleaned {
		old, ok := q.entries[name]
		delete(q.entries, name)
		if err := q.save(); err != nil {
			if ok {
				q.entries[name] = old
			}
			q.signal()
			return err
		}
	} else {
		q.signal()
	}
	return nil
}

func (q *CleanupQueue) signal() {
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

// Run retries pending entries at startup, immediately after a failure, then every minute.
func (q *CleanupQueue) Run(ctx context.Context, keys HostKeys) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		q.retry(ctx, keys)
		select {
		case <-ctx.Done():
			return
		case <-q.wake:
		case <-ticker.C:
		}
	}
}

func (q *CleanupQueue) retry(ctx context.Context, keys HostKeys) {
	q.mu.Lock()
	var pending []cleanupEntry
	for name, e := range q.entries {
		if !q.active[name] {
			q.active[name] = true
			pending = append(pending, e)
		}
	}
	q.mu.Unlock()
	for _, e := range pending {
		if ctx.Err() != nil {
			_ = q.release(e.Name, false)
			continue
		}
		attempt, cancel := context.WithTimeout(ctx, time.Minute)
		r, err := q.dial(boundedCleanupContext(attempt), e.Source, keys)
		if err == nil {
			err = removeSessionFiles(r, e.Source.Root, e.Name)
			abortRemote(r)
		}
		cancel()
		if err != nil {
			log.Printf("очистка временного скрипта %s на %s: %v; повтор через минуту", e.Name, e.Source.Host, err)
		}
		// Do not wake the worker on failure: the ticker controls retry frequency.
		q.mu.Lock()
		delete(q.active, e.Name)
		if err == nil {
			delete(q.entries, e.Name)
			if saveErr := q.save(); saveErr != nil {
				q.entries[e.Name] = e
				log.Printf("сохранение очереди очистки: %v", saveErr)
			} else {
				log.Printf("временные файлы %s на %s удалены", e.Name, e.Source.Host)
			}
		}
		q.mu.Unlock()
	}
}

// removeSessionFiles only deletes exact filenames and confirms that both are gone.
func removeSessionFiles(r Remote, root, name string) error {
	if !validScriptName(name) {
		return errors.New("неверное имя временного скрипта")
	}
	dir := root
	if dir == "" {
		dir = "/"
	}
	names, err := r.List(dir)
	if err != nil {
		return fmt.Errorf("список файлов: %w", err)
	}
	want := map[string]bool{name + ".php": true, name + ".list.php": true}
	for _, n := range names {
		if want[n] {
			if err := r.Remove(path.Join(root, n)); err != nil {
				return fmt.Errorf("удаление %s: %w", n, err)
			}
		}
	}
	names, err = r.List(dir)
	if err != nil {
		return fmt.Errorf("проверка удаления: %w", err)
	}
	for _, n := range names {
		if want[n] {
			return fmt.Errorf("файл %s остался на хостинге", n)
		}
	}
	return nil
}
