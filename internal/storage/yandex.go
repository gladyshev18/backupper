package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Yandex uses the Yandex Disk REST API (much faster than its throttled WebDAV).
type Yandex struct {
	token  string
	client *http.Client
	base   string
}

func (y *Yandex) do(ctx context.Context, method, endpoint string, q url.Values, out any) error {
	u := y.base + endpoint
	if q != nil {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "OAuth "+y.token)
	req.Header.Set("Accept", "application/json")
	resp, err := y.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 {
		return ErrNotFound
	}
	if resp.StatusCode >= 300 {
		return readErr(resp)
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

func diskPath(p string) string { return "disk:" + Clean(p) }

func (y *Yandex) MkdirAll(ctx context.Context, dir string) error {
	dir = Clean(dir)
	if dir == "/" {
		return nil
	}
	cur := ""
	for _, part := range strings.Split(strings.TrimPrefix(dir, "/"), "/") {
		cur += "/" + part
		err := y.do(ctx, http.MethodPut, "/resources", url.Values{"path": {diskPath(cur)}}, nil)
		if err != nil {
			if he, ok := err.(*httpError); ok && he.Status == 409 {
				continue // already exists
			}
			return fmt.Errorf("создание папки %s: %w", cur, err)
		}
	}
	return nil
}

type yLink struct {
	Href   string `json:"href"`
	Method string `json:"method"`
}

func (y *Yandex) Put(ctx context.Context, p string, r io.ReadSeeker, size int64) error {
	var link yLink
	if err := y.do(ctx, http.MethodGet, "/resources/upload", url.Values{"path": {diskPath(p)}, "overwrite": {"true"}}, &link); err != nil {
		return fmt.Errorf("получение ссылки загрузки: %w", err)
	}
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return err
	}
	method := link.Method
	if method == "" {
		method = http.MethodPut
	}
	req, err := http.NewRequestWithContext(ctx, method, link.Href, io.NopCloser(r))
	if err != nil {
		return err
	}
	req.ContentLength = size
	req.GetBody = func() (io.ReadCloser, error) {
		if _, err := r.Seek(0, io.SeekStart); err != nil {
			return nil, err
		}
		return io.NopCloser(r), nil
	}
	resp, err := y.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return readErr(resp)
	}
	return nil
}

func (y *Yandex) Get(ctx context.Context, p string) (io.ReadCloser, error) {
	var link yLink
	if err := y.do(ctx, http.MethodGet, "/resources/download", url.Values{"path": {diskPath(p)}}, &link); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, link.Href, nil)
	if err != nil {
		return nil, err
	}
	resp, err := y.client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		defer resp.Body.Close()
		return nil, readErr(resp)
	}
	return resp.Body, nil
}

func (y *Yandex) List(ctx context.Context, dir string) ([]Entry, error) {
	var out []Entry
	offset := 0
	for {
		var res struct {
			Embedded struct {
				Items []struct {
					Name     string    `json:"name"`
					Type     string    `json:"type"`
					Size     int64     `json:"size"`
					Modified time.Time `json:"modified"`
				} `json:"items"`
				Total int `json:"total"`
			} `json:"_embedded"`
		}
		q := url.Values{
			"path":   {diskPath(dir)},
			"limit":  {"1000"},
			"offset": {fmt.Sprint(offset)},
			"fields": {"_embedded.items.name,_embedded.items.type,_embedded.items.size,_embedded.items.modified,_embedded.total"},
		}
		if err := y.do(ctx, http.MethodGet, "/resources", q, &res); err != nil {
			return nil, err
		}
		for _, it := range res.Embedded.Items {
			out = append(out, Entry{Name: it.Name, IsDir: it.Type == "dir", Size: it.Size, Modified: it.Modified})
		}
		offset += len(res.Embedded.Items)
		if len(res.Embedded.Items) == 0 || offset >= res.Embedded.Total {
			return out, nil
		}
	}
}

func (y *Yandex) Delete(ctx context.Context, p string) error {
	err := y.do(ctx, http.MethodDelete, "/resources", url.Values{"path": {diskPath(p)}, "permanently": {"true"}}, nil)
	if err == ErrNotFound {
		return nil
	}
	return err
}

func (y *Yandex) Quota(ctx context.Context) (Quota, error) {
	var res struct {
		Total   int64 `json:"total_space"`
		Used    int64 `json:"used_space"`
		MaxFile int64 `json:"max_file_size"`
	}
	if err := y.do(ctx, http.MethodGet, "", url.Values{"fields": {"total_space,used_space,max_file_size"}}, &res); err != nil {
		return Quota{}, err
	}
	return Quota{Total: res.Total, Used: res.Used, MaxFile: res.MaxFile}, nil
}
