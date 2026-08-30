package chrome

import (
	"crypto/sha256"
	"database/sql"
	"path/filepath"
	"testing"
)

func TestWriteCookiesForChrome_AppBoundAndPreservesMeta(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Cookies")
	seedEmptyCookiesDB(t, path)
	db, err := sql.Open("sqlite3", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE meta (key LONGVARCHAR NOT NULL UNIQUE PRIMARY KEY, value LONGVARCHAR)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO meta(key, value) VALUES ('version', '24')`); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()

	host := ".facebook.com"
	value := "session-value"
	written, err := WriteCookiesForChrome(path, []Cookie{{
		HostKey: host, Name: "c_user", Value: value, Path: "/", IsSecure: 1, IsHTTPOnly: 1,
	}}, testKey)
	if err != nil {
		t.Fatalf("WriteCookiesForChrome: %v", err)
	}
	if written != 1 {
		t.Fatalf("written = %d", written)
	}

	db, err = sql.Open("sqlite3", "file:"+path+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var encrypted []byte
	if err := db.QueryRow(`SELECT encrypted_value FROM cookies WHERE host_key=? AND name='c_user'`, host).Scan(&encrypted); err != nil {
		t.Fatal(err)
	}
	plaintext, err := decryptValue(encrypted, testKey)
	if err != nil {
		t.Fatal(err)
	}
	prefix := sha256.Sum256([]byte(host))
	if len(plaintext) < sha256.Size || string(plaintext[:sha256.Size]) != string(prefix[:]) {
		t.Fatal("host-bound prefix missing")
	}
	if got := string(stripAppBoundPrefix([]byte(plaintext), host)); got != value {
		t.Fatalf("value = %q", got)
	}
	var version string
	if err := db.QueryRow(`SELECT value FROM meta WHERE key='version'`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != "24" {
		t.Fatalf("meta.version = %q, want 24", version)
	}
}
