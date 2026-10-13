package researchweb

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

// This reader neither acquires writer ownership nor runs migrations or recovery.
func ReadSQLiteResultSnapshot(dir string) (View, error) {
	path := filepath.Join(dir, "research.db")
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return View{}, errors.New("source SQLite database must be private and regular")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return View{}, err
	}
	dsn := (&url.URL{Scheme: "file", Path: abs, RawQuery: "mode=ro"}).String()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return View{}, err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return View{}, err
	}
	defer tx.Rollback()
	var raw []byte
	if err := tx.QueryRowContext(ctx, "SELECT data FROM current_state WHERE id=1").Scan(&raw); err != nil {
		return View{}, err
	}
	var view View
	if len(raw) > maxState || decodeJSON(raw, &view.Data) != nil {
		return view, ErrLimit
	}
	if err := validateState(view.Data); err != nil {
		return view, err
	}
	rows, err := tx.QueryContext(ctx, "SELECT metadata FROM history ORDER BY revision DESC LIMIT 100")
	if err != nil {
		return view, err
	}
	for rows.Next() {
		var body []byte
		var h History
		if err := rows.Scan(&body); err != nil {
			rows.Close()
			return view, err
		}
		if err := json.Unmarshal(body, &h); err != nil {
			rows.Close()
			return view, err
		}
		view.History = append(view.History, h)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return view, err
	}
	for i, j := 0, len(view.History)-1; i < j; i, j = i+1, j-1 {
		view.History[i], view.History[j] = view.History[j], view.History[i]
	}
	return view, tx.Commit()
}
