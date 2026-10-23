package desktop

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Microsoft/go-winio"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

// DefaultKeys are tried when no key file is set for a server.
func DefaultKeys() []string {
	home, _ := os.UserHomeDir()
	var out []string
	for _, n := range []string{"id_ed25519", "id_ecdsa", "id_rsa"} {
		p := filepath.Join(home, ".ssh", n)
		if _, err := os.Stat(p); err == nil {
			out = append(out, p)
		}
	}
	return out
}

func loadSigner(path, passphrase string) (ssh.Signer, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("не удалось прочитать ключ %s: %w", path, err)
	}
	if passphrase != "" {
		return ssh.ParsePrivateKeyWithPassphrase(b, []byte(passphrase))
	}
	s, err := ssh.ParsePrivateKey(b)
	var pe *ssh.PassphraseMissingError
	if errors.As(err, &pe) {
		return nil, fmt.Errorf("ключ %s защищён паролем — укажите его в настройках сервера", filepath.Base(path))
	}
	if err != nil {
		return nil, fmt.Errorf("ключ %s: %w", filepath.Base(path), err)
	}
	return s, nil
}

// agentSigners asks the Windows OpenSSH agent (ssh-add) for keys, if it runs.
func agentSigners() []ssh.Signer {
	timeout := 300 * time.Millisecond
	c, err := winio.DialPipe(`\\.\pipe\openssh-ssh-agent`, &timeout)
	if err != nil {
		return nil
	}
	signers, err := agent.NewClient(c).Signers()
	if err != nil {
		return nil
	}
	return signers
}

func authMethods(s Server) ([]ssh.AuthMethod, error) {
	var signers []ssh.Signer
	if s.KeyPath != "" {
		sg, err := loadSigner(s.KeyPath, s.Passphrase)
		if err != nil {
			return nil, err
		}
		signers = append(signers, sg)
	} else {
		var lastErr error
		for _, p := range DefaultKeys() {
			sg, err := loadSigner(p, s.Passphrase)
			if err != nil {
				lastErr = err
				continue
			}
			signers = append(signers, sg)
		}
		signers = append(signers, agentSigners()...)
		if len(signers) == 0 {
			if lastErr != nil {
				return nil, lastErr
			}
			return nil, errors.New("не найден SSH-ключ: укажите файл ключа в настройках сервера")
		}
	}
	return []ssh.AuthMethod{ssh.PublicKeys(signers...)}, nil
}

func Fingerprint(encodedKey string) string {
	b, err := base64.StdEncoding.DecodeString(encodedKey)
	if err != nil {
		return ""
	}
	k, err := ssh.ParsePublicKey(b)
	if err != nil {
		return ""
	}
	return ssh.FingerprintSHA256(k)
}

// Dial connects with the server's key. The host key is pinned on first use:
// seen returns the key presented by the server so the caller can save it.
func Dial(s Server) (client *ssh.Client, seen string, err error) {
	auth, err := authMethods(s)
	if err != nil {
		return nil, "", err
	}
	port := s.Port
	if port == 0 {
		port = 22
	}
	user := s.User
	if user == "" {
		user = "root"
	}
	cfg := &ssh.ClientConfig{
		User:    user,
		Auth:    auth,
		Timeout: 15 * time.Second,
		HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
			seen = base64.StdEncoding.EncodeToString(key.Marshal())
			if s.HostKey != "" && s.HostKey != seen {
				return fmt.Errorf("ключ сервера изменился (было %s, стало %s). Если сервер переустанавливали — сбросьте ключ в настройках сервера, иначе это может быть подмена", Fingerprint(s.HostKey), ssh.FingerprintSHA256(key))
			}
			return nil
		},
	}
	addr := net.JoinHostPort(s.Host, strconv.Itoa(port))
	client, err = ssh.Dial("tcp", addr, cfg)
	if err != nil {
		msg := err.Error()
		switch {
		case strings.Contains(msg, "unable to authenticate"):
			return nil, seen, fmt.Errorf("сервер не принял ключ для пользователя %s", user)
		case strings.Contains(msg, "i/o timeout"), strings.Contains(msg, "connectex"):
			return nil, seen, fmt.Errorf("нет соединения с %s: %v", addr, err)
		}
		return nil, seen, err
	}
	return client, seen, nil
}

// Exec runs a command and returns stdout; stderr goes into the error.
func Exec(c *ssh.Client, cmd string, stdin io.Reader) (string, error) {
	sess, err := c.NewSession()
	if err != nil {
		return "", err
	}
	defer sess.Close()
	var out, errb bytes.Buffer
	sess.Stdout, sess.Stderr = &out, &errb
	if stdin != nil {
		sess.Stdin = stdin
	}
	if err := sess.Run(cmd); err != nil {
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = err.Error()
		}
		return out.String(), errors.New(msg)
	}
	return out.String(), nil
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }
