package chrome

import (
	"database/sql"
	"fmt"
	"time"
)

// WriteCookiesForChrome writes the host-bound v10 plaintext shape expected by
// current Google Chrome. Unlike WriteCookies, it preserves Chrome's own
// meta.version and is intended only for a profile Chrome itself will open.
func WriteCookiesForChrome(dbPath string, cookies []Cookie, key []byte) (int, error) {
	db, err := sql.Open("sqlite3", "file:"+dbPath+"?_journal=WAL&_busy_timeout=2000")
	if err != nil {
		return 0, fmt.Errorf("open destination cookies db: %w", err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		return 0, fmt.Errorf("ping destination cookies db (is Chrome running?): %w", err)
	}
	cols, err := readTableColumns(db, "cookies")
	if err != nil {
		return 0, fmt.Errorf("read cookies schema: %w", err)
	}
	insertSQL, args := buildUpsert(cols)
	tx, err := db.Begin()
	if err != nil {
		return 0, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()
	stmt, err := tx.Prepare(insertSQL)
	if err != nil {
		return 0, fmt.Errorf("prepare upsert: %w", err)
	}
	defer stmt.Close()

	nowChrome := time.Now().UnixMicro() + chromeEpochDeltaMicros
	written := 0
	for _, c := range cookies {
		plaintext := prependAppBoundPrefix([]byte(c.Value), c.HostKey)
		encrypted, err := encryptValueBytes(plaintext, key)
		if err != nil {
			return written, fmt.Errorf("encrypt %s/%s: %w", c.HostKey, c.Name, err)
		}
		row := args(rowInput{cookie: c, encrypted: encrypted, creationUTC: nowChrome, lastUpdateUTC: nowChrome})
		if _, err := stmt.Exec(row...); err != nil {
			return written, fmt.Errorf("upsert %s/%s: %w", c.HostKey, c.Name, err)
		}
		written++
	}
	if err := tx.Commit(); err != nil {
		return written, fmt.Errorf("commit tx: %w", err)
	}
	return written, nil
}
