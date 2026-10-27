package site

import (
	"context"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
)

// Beget's bot protection: without its cookie GET gets a JavaScript page that sets
// it and POST gets 405 from nginx. The session must pass it and reach the script.
func TestPassesCookieChallenge(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie("beget"); err == nil && c.Value == "begetok" {
			w.WriteHeader(http.StatusTeapot) // "the script answered"
			return
		}
		if r.Method == http.MethodPost {
			http.Error(w, "<html><head><title>405 Not Allowed</title></head></html>", http.StatusMethodNotAllowed)
			return
		}
		w.Write([]byte(`<html><head><script>function set_cookie(){var now = new Date();var time = now.getTime();` +
			`time += 19360000 * 1000;now.setTime(time);document.cookie='beget=begetok'+'; expires='+now.toGMTString()+'; path=/';}` +
			`set_cookie();location.reload();;</script></head><body></body></html>`))
	}))
	defer srv.Close()

	jar, _ := cookiejar.New(nil)
	var logged []string
	s := &Session{
		url:    srv.URL + "/backupper_0123456789abcdef.php",
		http:   &http.Client{Jar: jar},
		macKey: []byte("k"),
		logf:   func(_, f string, _ ...any) { logged = append(logged, f) },
	}
	if err := s.checkClock(context.Background()); err != nil {
		t.Fatal(err)
	}
	resp, _, err := s.post(context.Background(), "info", "{}")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusTeapot {
		t.Fatalf("POST after the challenge: HTTP %d", resp.StatusCode)
	}
	if len(logged) != 1 || !strings.Contains(logged[0], "cookie") {
		t.Fatalf("the challenge must be logged once: %q", logged)
	}
	if s.passChallenge([]byte("<html><body>Bitrix page, location.reload()</body></html>")) {
		t.Fatal("a page without document.cookie is not a challenge")
	}
}
