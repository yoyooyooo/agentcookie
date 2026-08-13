package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/agentcookie/internal/chrome"
)

func TestCheckFilePermissions_0600(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cookies.json")
	if err := os.WriteFile(path, []byte(`[]`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := checkFilePermissions(path); err != nil {
		t.Fatalf("0600 should be allowed: %v", err)
	}
}

func TestCheckFilePermissions_0400(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cookies.json")
	if err := os.WriteFile(path, []byte(`[]`), 0o400); err != nil {
		t.Fatal(err)
	}
	if err := checkFilePermissions(path); err != nil {
		t.Fatalf("0400 should be allowed: %v", err)
	}
}

func TestCheckFilePermissions_WorldReadable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cookies.json")
	if err := os.WriteFile(path, []byte(`[]`), 0o644); err != nil {
		t.Fatal(err)
	}
	err := checkFilePermissions(path)
	if err == nil {
		t.Fatal("expected error for world-readable file")
	}
	if os.Getenv("TEST_VERBOSE") != "" {
		t.Logf("error message: %v", err)
	}
}

func TestCheckFilePermissions_GroupReadable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cookies.json")
	if err := os.WriteFile(path, []byte(`[]`), 0o640); err != nil {
		t.Fatal(err)
	}
	err := checkFilePermissions(path)
	if err == nil {
		t.Fatal("expected error for group-readable file")
	}
}

func TestToChromeCookies(t *testing.T) {
	expiry := int64(1800000000)
	input := []importCookie{
		{
			Domain:         ".github.com",
			Name:           "user_session",
			Value:          "secret",
			Path:           "/",
			Secure:         true,
			HTTPOnly:       true,
			SameSite:       "lax",
			ExpirationDate: &expiry,
		},
		{
			Domain:   "example.com",
			Name:     "session",
			Value:    "val",
			Path:     "/app",
			Secure:   false,
			HTTPOnly: false,
			SameSite: "no_restriction",
		},
	}

	cookies := toChromeCookies(input)
	if len(cookies) != 2 {
		t.Fatalf("expected 2 cookies, got %d", len(cookies))
	}

	c1 := cookies[0]
	if c1.HostKey != ".github.com" {
		t.Errorf("c1.HostKey = %q", c1.HostKey)
	}
	if c1.IsSecure != 1 || c1.IsHTTPOnly != 1 {
		t.Errorf("c1 flags: secure=%d httponly=%d", c1.IsSecure, c1.IsHTTPOnly)
	}
	if c1.SameSite != 1 {
		t.Errorf("c1.SameSite = %d, want 1 (lax)", c1.SameSite)
	}
	if c1.HasExpires != 1 || c1.IsPersistent != 1 {
		t.Errorf("c1 persistent: has_expires=%d is_persistent=%d", c1.HasExpires, c1.IsPersistent)
	}

	c2 := cookies[1]
	if c2.SameSite != 0 {
		t.Errorf("c2.SameSite = %d, want 0 (None)", c2.SameSite)
	}
	if c2.HasExpires != 0 {
		t.Errorf("session cookie should have HasExpires=0")
	}
}

func TestImportSameSite(t *testing.T) {
	cases := map[string]int{
		"no_restriction": 0,
		"lax":            1,
		"strict":         2,
		"unspecified":    -1,
		"":               -1,
		"unknown":        -1,
	}
	for input, want := range cases {
		if got := importSameSite(input); got != want {
			t.Errorf("importSameSite(%q) = %d, want %d", input, got, want)
		}
	}
}

func TestToChromeCookies_Empty(t *testing.T) {
	cookies := toChromeCookies(nil)
	if len(cookies) != 0 {
		t.Errorf("expected empty slice, got %d", len(cookies))
	}
}

func TestChromeCookieRoundTrip(t *testing.T) {
	original := chrome.Cookie{
		HostKey:    ".example.com",
		Name:       "test",
		Value:      "value",
		Path:       "/",
		IsSecure:   1,
		IsHTTPOnly: 1,
		SameSite:   1,
		ExpiresUTC: 13363527432123456,
		HasExpires: 1,
	}

	exported := toExportCookies([]chrome.Cookie{original})
	imported := toChromeCookies([]importCookie{
		{
			Domain:         exported[0].Domain,
			Name:           exported[0].Name,
			Value:          exported[0].Value,
			Path:           exported[0].Path,
			Secure:         exported[0].Secure,
			HTTPOnly:       exported[0].HTTPOnly,
			SameSite:       exported[0].SameSite,
			ExpirationDate: exported[0].ExpirationDate,
		},
	})

	if len(imported) != 1 {
		t.Fatalf("expected 1 cookie, got %d", len(imported))
	}

	c := imported[0]
	if c.HostKey != original.HostKey {
		t.Errorf("HostKey: got %q, want %q", c.HostKey, original.HostKey)
	}
	if c.Name != original.Name {
		t.Errorf("Name: got %q, want %q", c.Name, original.Name)
	}
	if c.IsSecure != original.IsSecure {
		t.Errorf("IsSecure: got %d, want %d", c.IsSecure, original.IsSecure)
	}
	if c.IsHTTPOnly != original.IsHTTPOnly {
		t.Errorf("IsHTTPOnly: got %d, want %d", c.IsHTTPOnly, original.IsHTTPOnly)
	}
	if c.SameSite != original.SameSite {
		t.Errorf("SameSite: got %d, want %d", c.SameSite, original.SameSite)
	}
}
