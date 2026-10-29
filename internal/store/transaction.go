package store

// Tx is valid only inside Transaction. Never call DB methods from its callback:
// the connection mutex is held until commit or rollback.
type Tx struct{ db *DB }

func (t *Tx) Query(sql string, args ...string) (Rows, error) { return t.db.query(sql, args...) }
func (t *Tx) Exec(sql string, args ...string) error {
	_, err := t.db.query(sql, args...)
	return err
}

// Transaction serializes reads and writes on the existing connection. Unlike
// ExecBatch it also supports checking a preview snapshot before changing data.
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
