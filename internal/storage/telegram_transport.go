package storage

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"

	"backupper/internal/model"
)

// tgTransport reaches the Bot API server and the files it downloads. In --local
// mode getFile returns a path on that server's disk instead of a URL.
type tgTransport interface {
	client() *http.Client
	base() string
	// open reads a downloaded file; Close removes it from the server.
	open(ctx context.Context, p string) (io.ReadCloser, error)
}

// directTransport talks to a Bot API server on this machine.
type directTransport struct {
	url string
	hc  *http.Client
}

func (d *directTransport) client() *http.Client { return d.hc }
func (d *directTransport) base() string         { return d.url }

func (d *directTransport) open(_ context.Context, p string) (io.ReadCloser, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	return &removeOnClose{r: f, close: f.Close, remove: func() error { return os.Remove(p) }}, nil
}

type removeOnClose struct {
	r      io.Reader
	close  func() error
	remove func() error
	once   sync.Once
}

func (r *removeOnClose) Read(p []byte) (int, error) { return r.r.Read(p) }

// WriteTo lets io.Copy use the SFTP client's parallel reads.
func (r *removeOnClose) WriteTo(w io.Writer) (int64, error) {
	if wt, ok := r.r.(io.WriterTo); ok {
		return wt.WriteTo(w)
	}
	return io.Copy(w, struct{ io.Reader }{r.r})
}

func (r *removeOnClose) Close() error {
	err := r.close()
	r.once.Do(func() {
		// The Bot API keeps downloads forever; parts are large and the disk is small.
		if e := r.remove(); e != nil && err == nil && !os.IsNotExist(e) {
			err = e
		}
	})
	return err
}

// sshTransport goes to the Bot API through an SSH connection as the restricted
// relay user: port forwarding to the API and SFTP for the downloaded files.
type sshTransport struct {
	cfg  *ssh.ClientConfig
	addr string
	api  string
	hc   *http.Client

	mu sync.Mutex
	c  *ssh.Client
	sc *sftp.Client
}

var (
	sshPoolMu sync.Mutex
	sshPool   = map[string]*sshTransport{}
)

func transportFor(ts *model.TelegramServer) (tgTransport, error) {
	if ts == nil {
		return nil, errors.New("не настроен сервер Bot API")
	}
	if ts.DirectURL != "" {
		return &directTransport{url: strings.TrimRight(ts.DirectURL, "/"), hc: httpClient()}, nil
	}
	if ts.Host == "" {
		return nil, errors.New("не указан адрес сервера Bot API")
	}
	if ts.Key == "" || ts.HostKey == "" {
		return nil, errors.New("сервер Bot API ещё не установлен из панели: нет ключа доступа")
	}
	port, apiPort, user := ts.Port, ts.APIPort, ts.User
	if port == 0 {
		port = 22
	}
	if apiPort == 0 {
		apiPort = 8081
	}
	if user == "" {
		user = model.TelegramRelayUser
	}
	sum := sha256.Sum256([]byte(ts.Key + "|" + ts.HostKey))
	id := fmt.Sprintf("%s|%d|%s|%d|%s", ts.Host, port, user, apiPort, hex.EncodeToString(sum[:8]))
	sshPoolMu.Lock()
	defer sshPoolMu.Unlock()
	if t := sshPool[id]; t != nil {
		return t, nil
	}
	signer, err := ssh.ParsePrivateKey([]byte(ts.Key))
	if err != nil {
		return nil, fmt.Errorf("ключ доступа к серверу Bot API: %w", err)
	}
	pinned := ts.HostKey
	t := &sshTransport{
		addr: net.JoinHostPort(ts.Host, strconv.Itoa(port)),
		api:  "127.0.0.1:" + strconv.Itoa(apiPort),
		cfg: &ssh.ClientConfig{
			User:    user,
			Auth:    []ssh.AuthMethod{ssh.PublicKeys(signer)},
			Timeout: 30 * time.Second,
			HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
				if base64.StdEncoding.EncodeToString(key.Marshal()) != pinned {
					return fmt.Errorf("ключ сервера Bot API изменился (%s) — проверьте сервер и переустановите Bot API из панели", ssh.FingerprintSHA256(key))
				}
				return nil
			},
		},
	}
	t.hc = &http.Client{Transport: &http.Transport{
		DialContext:           t.dialAPI,
		MaxIdleConnsPerHost:   4,
		IdleConnTimeout:       60 * time.Second,
		ExpectContinueTimeout: 5 * time.Second,
	}}
	sshPool[id] = t
	return t, nil
}

func (t *sshTransport) client() *http.Client { return t.hc }

// Requests go to the forwarded port; the host in the URL is not used.
func (t *sshTransport) base() string { return "http://" + t.api }

func (t *sshTransport) conn(ctx context.Context) (*ssh.Client, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.c != nil {
		return t.c, nil
	}
	d := net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}
	nc, err := d.DialContext(ctx, "tcp", t.addr)
	if err != nil {
		return nil, fmt.Errorf("сервер Bot API %s недоступен: %w", t.addr, err)
	}
	cc, chans, reqs, err := ssh.NewClientConn(nc, t.addr, t.cfg)
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("SSH к серверу Bot API: %w", err)
	}
	c := ssh.NewClient(cc, chans, reqs)
	t.c = c
	go t.keepalive(c)
	return c, nil
}

// keepalive notices a dead connection (NAT timeouts, server restarts) so the
// next request dials again instead of hanging.
func (t *sshTransport) keepalive(c *ssh.Client) {
	done := make(chan struct{})
	go func() { c.Wait(); close(done) }()
	tick := time.NewTicker(30 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-done:
			t.drop(c)
			return
		case <-tick.C:
			ok := make(chan bool, 1)
			go func() { _, _, err := c.SendRequest("keepalive@openssh.com", true, nil); ok <- err == nil }()
			select {
			case good := <-ok:
				if good {
					continue
				}
			case <-time.After(20 * time.Second):
			}
			c.Close()
		}
	}
}

func (t *sshTransport) drop(c *ssh.Client) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.c == c {
		t.c = nil
		if t.sc != nil {
			t.sc.Close()
			t.sc = nil
		}
	}
}

func (t *sshTransport) dialAPI(ctx context.Context, _, _ string) (net.Conn, error) {
	for attempt := 0; ; attempt++ {
		c, err := t.conn(ctx)
		if err != nil {
			return nil, err
		}
		nc, err := c.Dial("tcp", t.api)
		if err == nil {
			return nc, nil
		}
		if attempt > 0 {
			return nil, fmt.Errorf("Bot API на сервере не отвечает (%s): %w — проверьте службу backupper-tg-bot-api", t.api, err)
		}
		c.Close()
		t.drop(c)
	}
}

func (t *sshTransport) sftp(ctx context.Context) (*sftp.Client, error) {
	c, err := t.conn(ctx)
	if err != nil {
		return nil, err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.sc == nil {
		sc, err := sftp.NewClient(c)
		if err != nil {
			return nil, fmt.Errorf("SFTP на сервере Bot API: %w", err)
		}
		t.sc = sc
	}
	return t.sc, nil
}

func (t *sshTransport) open(ctx context.Context, p string) (io.ReadCloser, error) {
	if !path.IsAbs(p) {
		return nil, fmt.Errorf("неожиданный путь файла %q от Bot API", p)
	}
	sc, err := t.sftp(ctx)
	if err != nil {
		return nil, err
	}
	f, err := sc.Open(p)
	if err != nil {
		return nil, fmt.Errorf("чтение %s на сервере Bot API: %w", path.Base(p), err)
	}
	return &removeOnClose{r: f, close: f.Close, remove: func() error { return sc.Remove(p) }}, nil
}
