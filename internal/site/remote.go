package site

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/jlaffaye/ftp"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"

	"backupper/internal/model"
)

// Remote is the file access to the hosting: just enough to put the script and remove it.
type Remote interface {
	Upload(p string, data []byte) error
	Remove(p string) error
	List(dir string) ([]string, error)
	Close() error
}

// HostKeys pins SFTP host keys on first use.
type HostKeys interface {
	Check(hostport string, key ssh.PublicKey) error
}

func defaultPort(s model.SiteSource) int {
	if s.Port > 0 {
		return s.Port
	}
	if s.Protocol == model.ProtoSFTP {
		return 22
	}
	return 21
}

// Dial connects to the hosting with the protocol chosen in the source.
func Dial(ctx context.Context, s model.SiteSource, keys HostKeys) (Remote, error) {
	addr := net.JoinHostPort(s.Host, strconv.Itoa(defaultPort(s)))
	if s.Host == "" {
		return nil, errors.New("не указан адрес FTP-сервера")
	}
	switch s.Protocol {
	case model.ProtoSFTP:
		return dialSFTP(ctx, addr, s, keys)
	case model.ProtoFTP, model.ProtoFTPS, "":
		return dialFTP(ctx, addr, s)
	}
	return nil, fmt.Errorf("неизвестный протокол %q", s.Protocol)
}

type ftpRemote struct{ c *ftp.ServerConn }

func dialFTP(ctx context.Context, addr string, s model.SiteSource) (Remote, error) {
	opts := []ftp.DialOption{ftp.DialWithContext(ctx), ftp.DialWithTimeout(30 * time.Second)}
	if s.Protocol == model.ProtoFTPS {
		cfg := &tls.Config{
			ServerName:         s.Host,
			InsecureSkipVerify: s.Insecure,
			// Many servers require the data connection to resume the control TLS session.
			ClientSessionCache: tls.NewLRUClientSessionCache(4),
		}
		if defaultPort(s) == 990 {
			opts = append(opts, ftp.DialWithTLS(cfg))
		} else {
			opts = append(opts, ftp.DialWithExplicitTLS(cfg))
		}
	}
	c, err := ftp.Dial(addr, opts...)
	if err != nil {
		return nil, friendlyTLS(fmt.Errorf("подключение к %s: %w", addr, err))
	}
	if err := c.Login(s.User, s.Password); err != nil {
		c.Quit()
		return nil, fmt.Errorf("FTP не принял логин или пароль: %w", err)
	}
	return &ftpRemote{c: c}, nil
}

func friendlyTLS(err error) error {
	msg := err.Error()
	if strings.Contains(msg, "certificate") || strings.Contains(msg, "x509") {
		return fmt.Errorf("%w — сертификат хостинга не подходит к адресу; включите «Не проверять сертификат»", err)
	}
	return err
}

func (r *ftpRemote) Upload(p string, data []byte) error { return r.c.Stor(p, bytes.NewReader(data)) }
func (r *ftpRemote) Remove(p string) error              { return r.c.Delete(p) }
func (r *ftpRemote) Close() error                       { return r.c.Quit() }

func (r *ftpRemote) List(dir string) ([]string, error) {
	entries, err := r.c.List(dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.Name != "." && e.Name != ".." {
			out = append(out, path.Base(e.Name))
		}
	}
	return out, nil
}

type sftpRemote struct {
	ssh *ssh.Client
	c   *sftp.Client
}

func dialSFTP(ctx context.Context, addr string, s model.SiteSource, keys HostKeys) (Remote, error) {
	pass := s.Password
	cfg := &ssh.ClientConfig{
		User: s.User,
		Auth: []ssh.AuthMethod{
			ssh.Password(pass),
			// Some hostings only offer keyboard-interactive with a single password prompt.
			ssh.KeyboardInteractive(func(_, _ string, qs []string, _ []bool) ([]string, error) {
				ans := make([]string, len(qs))
				for i := range ans {
					ans[i] = pass
				}
				return ans, nil
			}),
		},
		Timeout: 30 * time.Second,
		HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
			if keys == nil {
				return nil
			}
			return keys.Check(addr, key)
		},
	}
	d := net.Dialer{Timeout: 30 * time.Second}
	nc, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("подключение к %s: %w", addr, err)
	}
	cc, chans, reqs, err := ssh.NewClientConn(nc, addr, cfg)
	if err != nil {
		nc.Close()
		if strings.Contains(err.Error(), "unable to authenticate") {
			return nil, errors.New("SFTP не принял логин или пароль")
		}
		return nil, err
	}
	client := ssh.NewClient(cc, chans, reqs)
	sc, err := sftp.NewClient(client)
	if err != nil {
		client.Close()
		return nil, fmt.Errorf("SFTP недоступен на этом сервере: %w", err)
	}
	return &sftpRemote{ssh: client, c: sc}, nil
}

func (r *sftpRemote) Upload(p string, data []byte) error {
	f, err := r.c.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_TRUNC)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return r.c.Chmod(p, 0o644)
}

func (r *sftpRemote) Remove(p string) error { return r.c.Remove(p) }

func (r *sftpRemote) List(dir string) ([]string, error) {
	fis, err := r.c.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(fis))
	for _, fi := range fis {
		out = append(out, fi.Name())
	}
	return out, nil
}

func (r *sftpRemote) Close() error {
	r.c.Close()
	return r.ssh.Close()
}
