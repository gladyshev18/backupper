// Package storage talks to the cloud drives: Yandex Disk (REST API or WebDAV),
// Mail.ru Cloud (WebDAV) and any other WebDAV server.
package storage

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"path"
	"strings"
	"time"

	"backupper/internal/model"
)

var ErrNotFound = errors.New("не найдено")

type Entry struct {
	Name     string
	IsDir    bool
	Size     int64
	Modified time.Time
}

type Quota struct {
	Total   int64
	Used    int64
	MaxFile int64 // 0 = unknown / unlimited
}

type Storage interface {
	MkdirAll(ctx context.Context, dir string) error
	Put(ctx context.Context, p string, r io.ReadSeeker, size int64) error
	Get(ctx context.Context, p string) (io.ReadCloser, error)
	List(ctx context.Context, dir string) ([]Entry, error)
	Delete(ctx context.Context, p string) error
	Quota(ctx context.Context) (Quota, error)
}

func httpClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			DialContext:           (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
			TLSHandshakeTimeout:   30 * time.Second,
			MaxIdleConnsPerHost:   4,
			IdleConnTimeout:       90 * time.Second,
			ExpectContinueTimeout: 5 * time.Second,
		},
	}
}

// New builds a client for the storage description.
func New(s model.Storage) (Storage, error) {
	switch s.Kind {
	case model.StorageYandex:
		if s.Token == "" {
			return nil, fmt.Errorf("не указан OAuth-токен Яндекс Диска")
		}
		return &Yandex{token: s.Token, client: httpClient(), base: "https://cloud-api.yandex.net/v1/disk"}, nil
	case model.StorageYandexWebDAV:
		return newWebDAV("https://webdav.yandex.ru", s.Login, s.Password)
	case model.StorageMailru:
		return newWebDAV("https://webdav.cloud.mail.ru", s.Login, s.Password)
	case model.StorageWebDAV:
		if s.URL == "" {
			return nil, fmt.Errorf("не указан адрес WebDAV")
		}
		return newWebDAV(s.URL, s.Login, s.Password)
	}
	return nil, fmt.Errorf("неизвестный тип хранилища %q", s.Kind)
}

// KnownMaxFile returns a per-file limit known in advance for a provider.
func KnownMaxFile(kind string) int64 {
	if kind == model.StorageMailru {
		return 2 << 30 // free Mail.ru plans reject files over 2 GB
	}
	return 0
}

// Clean normalizes a cloud path: always absolute, forward slashes, no trailing slash.
func Clean(p string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	p = path.Clean("/" + p)
	return p
}

type httpError struct {
	Status int
	Body   string
}

func (e *httpError) Error() string {
	b := strings.TrimSpace(e.Body)
	if len(b) > 300 {
		b = b[:300]
	}
	switch e.Status {
	case 401, 403:
		return fmt.Sprintf("доступ запрещён (HTTP %d): проверьте токен/пароль приложения. %s", e.Status, b)
	case 507:
		return "в облаке закончилось место (HTTP 507)"
	case 413:
		return "облако отклонило файл: слишком большой (HTTP 413) — уменьшите размер куска"
	}
	return fmt.Sprintf("HTTP %d: %s", e.Status, b)
}

func readErr(resp *http.Response) error {
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return &httpError{Status: resp.StatusCode, Body: string(b)}
}

// IsRetryable tells whether an upload error is worth repeating.
func IsRetryable(err error) bool {
	var he *httpError
	if errors.As(err, &he) {
		switch he.Status {
		case 401, 403, 404, 409, 413, 507:
			return false
		}
	}
	return !errors.Is(err, context.Canceled)
}

// IsTooLarge reports whether the cloud rejected the payload because of its size.
func IsTooLarge(err error) bool {
	var he *httpError
	return errors.As(err, &he) && he.Status == 413
}

// Test checks credentials and, when speedBytes > 0, measures upload speed
// by sending a throwaway file of that size into the storage root.
func Test(ctx context.Context, s model.Storage, speedBytes int64) model.StorageTest {
	var res model.StorageTest
	st, err := New(s)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	q, err := st.Quota(ctx)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	res.Total, res.Used, res.MaxFile = q.Total, q.Used, q.MaxFile
	if res.MaxFile == 0 {
		res.MaxFile = KnownMaxFile(s.Kind)
	}
	root := Clean(s.Root)
	if err := st.MkdirAll(ctx, root); err != nil {
		res.Error = err.Error()
		return res
	}
	if speedBytes > 0 {
		buf := make([]byte, speedBytes)
		rand.Read(buf) // random data so nothing on the way can compress it
		p := path.Join(root, ".backupper-speedtest.bin")
		start := time.Now()
		if err := st.Put(ctx, p, bytes.NewReader(buf), speedBytes); err != nil {
			res.Error = "тестовая загрузка: " + err.Error()
			return res
		}
		el := time.Since(start)
		res.ElapsedMs = el.Milliseconds()
		res.SpeedBps = float64(speedBytes) / el.Seconds()
		_ = st.Delete(ctx, p)
	}
	res.OK = true
	return res
}
