package store

/*
#cgo LDFLAGS: -lsqlite3
#include <sqlite3.h>
#include <stdlib.h>
static int fh_bind_text(sqlite3_stmt *st, int n, const char *v) { return sqlite3_bind_text(st,n,v,-1,SQLITE_TRANSIENT); }
*/
import "C"

import (
	"errors"
	"fmt"
	"sync"
	"unsafe"
)

type DB struct {
	mu  sync.Mutex
	ptr *C.sqlite3
}
type Rows []map[string]string

func Open(path string) (*DB, error) {
	cs := C.CString(path)
	defer C.free(unsafe.Pointer(cs))
	var p *C.sqlite3
	rc := C.sqlite3_open_v2(cs, &p, C.SQLITE_OPEN_READWRITE|C.SQLITE_OPEN_CREATE|C.SQLITE_OPEN_FULLMUTEX, nil)
	if rc != C.SQLITE_OK {
		msg := "sqlite open failed"
		if p != nil {
			msg = C.GoString(C.sqlite3_errmsg(p))
			C.sqlite3_close(p)
		}
		return nil, errors.New(msg)
	}
	d := &DB{ptr: p}
	C.sqlite3_busy_timeout(p, 5000)
	if err := d.Exec("PRAGMA journal_mode=DELETE"); err != nil {
		d.Close()
		return nil, err
	}
	if err := d.Exec("PRAGMA foreign_keys=ON"); err != nil {
		d.Close()
		return nil, err
	}
	return d, nil
}
func (d *DB) Close() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.ptr != nil {
		C.sqlite3_close(d.ptr)
		d.ptr = nil
	}
}
func (d *DB) Error() error { return errors.New(C.GoString(C.sqlite3_errmsg(d.ptr))) }
func (d *DB) query(sql string, args ...string) (Rows, error) {
	if d.ptr == nil {
		return nil, errors.New("db closed")
	}
	q := C.CString(sql)
	defer C.free(unsafe.Pointer(q))
	var st *C.sqlite3_stmt
	if C.sqlite3_prepare_v2(d.ptr, q, -1, &st, nil) != C.SQLITE_OK {
		return nil, d.Error()
	}
	defer C.sqlite3_finalize(st)
	for i, a := range args {
		v := C.CString(a)
		rc := C.fh_bind_text(st, C.int(i+1), v)
		C.free(unsafe.Pointer(v))
		if rc != C.SQLITE_OK {
			return nil, d.Error()
		}
	}
	var res Rows
	for {
		rc := C.sqlite3_step(st)
		if rc == C.SQLITE_DONE {
			break
		}
		if rc != C.SQLITE_ROW {
			return nil, d.Error()
		}
		row := map[string]string{}
		n := int(C.sqlite3_column_count(st))
		for i := 0; i < n; i++ {
			name := C.GoString(C.sqlite3_column_name(st, C.int(i)))
			if C.sqlite3_column_type(st, C.int(i)) == C.SQLITE_NULL {
				row[name] = ""
			} else {
				row[name] = C.GoString((*C.char)(unsafe.Pointer(C.sqlite3_column_text(st, C.int(i)))))
			}
		}
		res = append(res, row)
	}
	return res, nil
}
func (d *DB) Query(sql string, args ...string) (Rows, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.query(sql, args...)
}
func (d *DB) Exec(sql string, args ...string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	_, err := d.query(sql, args...)
	return err
}
func (d *DB) Init() error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS people(id TEXT PRIMARY KEY,name TEXT NOT NULL,color TEXT NOT NULL,notify_service TEXT NOT NULL DEFAULT '',position INTEGER NOT NULL DEFAULT 0)`,
		`CREATE TABLE IF NOT EXISTS events(id TEXT PRIMARY KEY,title TEXT NOT NULL,start_at TEXT NOT NULL,end_at TEXT NOT NULL,kind TEXT NOT NULL DEFAULT 'aftale',person_id TEXT NOT NULL DEFAULT '',pickup_by TEXT NOT NULL DEFAULT '',location TEXT NOT NULL DEFAULT '',notes TEXT NOT NULL DEFAULT '',created_at TEXT NOT NULL DEFAULT '')`,
		`CREATE INDEX IF NOT EXISTS events_dates ON events(start_at,end_at)`,
		`CREATE TABLE IF NOT EXISTS tasks(id TEXT PRIMARY KEY,title TEXT NOT NULL,assigned_to TEXT NOT NULL DEFAULT '',due_date TEXT NOT NULL,repeat_rule TEXT NOT NULL DEFAULT 'none')`,
		`CREATE TABLE IF NOT EXISTS task_templates(id TEXT PRIMARY KEY,title TEXT NOT NULL,assigned_to TEXT NOT NULL DEFAULT '',repeat_rule TEXT NOT NULL DEFAULT 'none')`,
		`CREATE TABLE IF NOT EXISTS calendar_owners(entity_id TEXT PRIMARY KEY,person_id TEXT NOT NULL DEFAULT '')`,
		`CREATE TABLE IF NOT EXISTS completions(task_id TEXT NOT NULL,day TEXT NOT NULL,PRIMARY KEY(task_id,day),FOREIGN KEY(task_id) REFERENCES tasks(id) ON DELETE CASCADE)`,
		`CREATE TABLE IF NOT EXISTS sessions(id TEXT PRIMARY KEY,expires_at TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS accounts(id TEXT PRIMARY KEY,username TEXT NOT NULL UNIQUE COLLATE NOCASE,password_hash TEXT NOT NULL,person_id TEXT NOT NULL DEFAULT '',enabled TEXT NOT NULL DEFAULT '1')`,
		`CREATE TABLE IF NOT EXISTS reminders(reminder_key TEXT PRIMARY KEY,sent_at TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS ai_settings(id INTEGER PRIMARY KEY CHECK(id=1),provider TEXT NOT NULL DEFAULT 'off',model TEXT NOT NULL DEFAULT '',base_url TEXT NOT NULL DEFAULT '',api_key_cipher TEXT NOT NULL DEFAULT '',reminders_enabled TEXT NOT NULL DEFAULT '0',reminder_time TEXT NOT NULL DEFAULT '18:00')`,
	}
	for _, s := range statements {
		if err := d.Exec(s); err != nil {
			return fmt.Errorf("migration: %w", err)
		}
	}
	// Existing deployments already have sessions: add account linkage without
	// replacing the database or ending legacy administrator sessions.
	cols, err := d.Query("PRAGMA table_info(sessions)")
	if err != nil { return fmt.Errorf("migration sessions columns: %w", err) }
	hasAccountID := false
	for _, col := range cols { if col["name"] == "account_id" { hasAccountID = true; break } }
	if !hasAccountID {
		if err := d.Exec("ALTER TABLE sessions ADD COLUMN account_id TEXT NOT NULL DEFAULT ''"); err != nil {
			return fmt.Errorf("migration sessions account_id: %w", err)
		}
	}
	return nil
}
