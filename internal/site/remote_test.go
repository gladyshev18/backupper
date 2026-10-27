package site

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// A hosting certificate for *.sweb.ru reached by IP: the error must name the
// certificate, not blame the login (explicit FTPS fails inside Login).
func TestFriendlyTLSNamesCertificate(t *testing.T) {
	cert := &x509.Certificate{DNSNames: []string{"*.sweb.ru", "sweb.ru"}}
	raw := fmt.Errorf("read: %w", &tls.CertificateVerificationError{Err: x509.HostnameError{Certificate: cert, Host: "192.0.2.1"}})
	got := friendlyTLS(raw).Error()
	for _, want := range []string{"*.sweb.ru", "192.0.2.1", "Не проверять сертификат"} {
		if !strings.Contains(got, want) {
			t.Fatalf("message %q lacks %q", got, want)
		}
	}
	if plain := errors.New("530 Login incorrect"); friendlyTLS(plain) != plain {
		t.Fatal("a non-TLS error must pass through unchanged")
	}
}
