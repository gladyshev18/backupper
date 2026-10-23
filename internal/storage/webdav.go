package storage

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"
)

// WebDAV works with Mail.ru Cloud, Yandex Disk WebDAV, Nextcloud and others.
type WebDAV struct {
	base   *url.URL
	login  string
	pass   string
	client *http.Client
}

func newWebDAV(base, login, pass string) (*WebDAV, error) {
	u, err := url.Parse(strings.TrimRight(base, "/"))
	if err != nil {
		return nil, fmt.Errorf("неверный адрес WebDAV: %w", err)
	}
	if login == "" || pass == "" {
		return nil, fmt.Errorf("не указан логин или пароль приложения")
	}
	return &WebDAV{base: u, login: login, pass: pass, client: httpClient()}, nil
}

func (w *WebDAV) url(p string) string {
	u := *w.base
	u.Path = strings.TrimRight(w.base.Path, "/") + Clean(p)
	return u.String()
}

func (w *WebDAV) req(ctx context.Context, method, p string, body io.Reader, hdr map[string]string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, w.url(p), body)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(w.login, w.pass)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	return w.client.Do(req)
}

func (w *WebDAV) MkdirAll(ctx context.Context, dir string) error {
	dir = Clean(dir)
	if dir == "/" {
		return nil
	}
	cur := ""
	for _, part := range strings.Split(strings.TrimPrefix(dir, "/"), "/") {
		cur += "/" + part
		resp, err := w.req(ctx, "MKCOL", cur+"/", nil, nil)
		if err != nil {
			return err
		}
		resp.Body.Close()
		// 405 / 409 / 301 mean the collection already exists on most servers.
		if resp.StatusCode >= 300 && resp.StatusCode != 405 && resp.StatusCode != 301 && resp.StatusCode != 409 {
			return fmt.Errorf("создание папки %s: %w", cur, &httpError{Status: resp.StatusCode})
		}
	}
	return nil
}

func (w *WebDAV) Put(ctx context.Context, p string, r io.ReadSeeker, size int64) error {
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, w.url(p), io.NopCloser(r))
	if err != nil {
		return err
	}
	req.SetBasicAuth(w.login, w.pass)
	req.ContentLength = size
	req.Header.Set("Content-Type", "application/octet-stream")
	req.GetBody = func() (io.ReadCloser, error) {
		if _, err := r.Seek(0, io.SeekStart); err != nil {
			return nil, err
		}
		return io.NopCloser(r), nil
	}
	resp, err := w.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return readErr(resp)
	}
	return nil
}

func (w *WebDAV) Get(ctx context.Context, p string) (io.ReadCloser, error) {
	resp, err := w.req(ctx, http.MethodGet, p, nil, nil)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == 404 {
		resp.Body.Close()
		return nil, ErrNotFound
	}
	if resp.StatusCode >= 300 {
		defer resp.Body.Close()
		return nil, readErr(resp)
	}
	return resp.Body, nil
}

type msResponse struct {
	Href     string `xml:"href"`
	Propstat []struct {
		Prop struct {
			ResourceType struct {
				Collection *struct{} `xml:"collection"`
			} `xml:"resourcetype"`
			Length    string `xml:"getcontentlength"`
			Modified  string `xml:"getlastmodified"`
			Available string `xml:"quota-available-bytes"`
			Used      string `xml:"quota-used-bytes"`
		} `xml:"prop"`
		Status string `xml:"status"`
	} `xml:"propstat"`
}

type multistatus struct {
	Responses []msResponse `xml:"response"`
}

func (w *WebDAV) propfind(ctx context.Context, p, depth, body string) (*multistatus, error) {
	resp, err := w.req(ctx, "PROPFIND", p, strings.NewReader(body), map[string]string{"Depth": depth, "Content-Type": "application/xml; charset=utf-8"})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 {
		return nil, ErrNotFound
	}
	if resp.StatusCode != 207 {
		return nil, readErr(resp)
	}
	var ms multistatus
	if err := xml.NewDecoder(resp.Body).Decode(&ms); err != nil {
		return nil, fmt.Errorf("разбор ответа WebDAV: %w", err)
	}
	return &ms, nil
}

const listBody = `<?xml version="1.0" encoding="utf-8"?><d:propfind xmlns:d="DAV:"><d:prop><d:resourcetype/><d:getcontentlength/><d:getlastmodified/></d:prop></d:propfind>`

func (w *WebDAV) List(ctx context.Context, dir string) ([]Entry, error) {
	dir = Clean(dir)
	ms, err := w.propfind(ctx, dir+"/", "1", listBody)
	if err != nil {
		return nil, err
	}
	self := strings.TrimRight(strings.TrimRight(w.base.Path, "/")+dir, "/")
	var out []Entry
	for _, r := range ms.Responses {
		href := r.Href
		if u, err := url.Parse(href); err == nil {
			href = u.Path
		}
		if dec, err := url.PathUnescape(href); err == nil {
			href = dec
		}
		href = strings.TrimRight(href, "/")
		if href == self || href == "" {
			continue
		}
		e := Entry{Name: path.Base(href)}
		for _, ps := range r.Propstat {
			if !strings.Contains(ps.Status, "200") {
				continue
			}
			if ps.Prop.ResourceType.Collection != nil {
				e.IsDir = true
			}
			if ps.Prop.Length != "" {
				e.Size, _ = strconv.ParseInt(ps.Prop.Length, 10, 64)
			}
			if ps.Prop.Modified != "" {
				e.Modified, _ = time.Parse(time.RFC1123, ps.Prop.Modified)
			}
		}
		out = append(out, e)
	}
	return out, nil
}

func (w *WebDAV) Delete(ctx context.Context, p string) error {
	resp, err := w.req(ctx, http.MethodDelete, p, nil, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 || resp.StatusCode < 300 {
		return nil
	}
	return readErr(resp)
}

const quotaBody = `<?xml version="1.0" encoding="utf-8"?><d:propfind xmlns:d="DAV:"><d:prop><d:quota-available-bytes/><d:quota-used-bytes/></d:prop></d:propfind>`

func (w *WebDAV) Quota(ctx context.Context) (Quota, error) {
	ms, err := w.propfind(ctx, "/", "0", quotaBody)
	if err != nil {
		return Quota{}, err
	}
	var q Quota
	for _, r := range ms.Responses {
		for _, ps := range r.Propstat {
			avail, _ := strconv.ParseInt(ps.Prop.Available, 10, 64)
			used, _ := strconv.ParseInt(ps.Prop.Used, 10, 64)
			if avail > 0 || used > 0 {
				q.Used = used
				q.Total = used + avail
			}
		}
	}
	return q, nil
}
