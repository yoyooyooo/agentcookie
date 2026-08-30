package sidecar

import (
	"path/filepath"
	"testing"

	"github.com/mvanhorn/agentcookie/internal/chrome"
)

func TestReadSidecarPreservesChromeCookieMetadata(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cookies-plain.db")
	want := chrome.Cookie{
		HostKey:       ".example.com",
		Name:          "session",
		Value:         "secret",
		Path:          "/app",
		ExpiresUTC:    13380163200000000,
		IsSecure:      1,
		IsHTTPOnly:    1,
		LastAccessUTC: 13370000000000000,
		HasExpires:    1,
		IsPersistent:  1,
		Priority:      2,
		SameSite:      2,
		SourceScheme:  2,
		SourcePort:    443,
	}
	if _, err := chrome.WriteCookiesSidecar(path, []chrome.Cookie{want}, nil); err != nil {
		t.Fatalf("WriteCookiesSidecar: %v", err)
	}

	got, err := ReadSidecar(path)
	if err != nil {
		t.Fatalf("ReadSidecar: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("cookies = %d, want 1", len(got))
	}
	cookie := got[0]
	if cookie.HostKey != want.HostKey || cookie.Name != want.Name || cookie.Value != want.Value ||
		cookie.Path != want.Path || cookie.ExpiresUTC != want.ExpiresUTC ||
		!cookie.IsSecure || !cookie.IsHTTPOnly || cookie.LastAccessUTC != want.LastAccessUTC ||
		!cookie.HasExpires || !cookie.IsPersistent || cookie.Priority != want.Priority ||
		cookie.SameSite != want.SameSite || cookie.SourceScheme != want.SourceScheme ||
		cookie.SourcePort != want.SourcePort {
		t.Fatalf("metadata mismatch: got %+v, want %+v", cookie, want)
	}
}
