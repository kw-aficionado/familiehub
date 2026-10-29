package store

// Tx holds the database lock for a read/validate/write operation. It must not be
// retained or shared outside Transaction. Use Tx methods, not DB methods, inside
// the callback (DB methods would attempt to lock the same connection again).
type Tx struct{ db *DB }

func (t *Tx) Query(sql string, args ...string) (Rows, error) {
	return t.db.query(sql, args...)
}
func (t *Tx) Exec(sql string, args ...string) error {
	_, err := t.db.query(sql, args...)
	return err
}

// Transaction serializes validation and writes against all other DB operations.
// Failed callbacks and panics roll back; a successful callback commits once.
func (d *DB) Transaction(fn func(*Tx) error) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, err := d.query("BEGIN IMMEDIATE"); err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = d.query("ROLLBACK")
		}
	}()
	if err := fn(&Tx{db: d}); err != nil {
		return err
	}
	if _, err := d.query("COMMIT"); err != nil {
		return err
	}
	committed = true
	return nil
}
