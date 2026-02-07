package store

import (
	"database/sql"
)

type SQLStore struct {
	db *sql.DB
}

func OpenSQLStore(dn, dsn string) (*SQLStore, error) {
	sqlDB, err := sql.Open(dn, dsn)
	if err != nil {
		return nil, err
	}

	if err = sqlDB.Ping(); err != nil {
		_ = sqlDB.Close()

		return nil, err
	}

	return &SQLStore{db: sqlDB}, nil
}

func (s *SQLStore) SetMaxOpenConns(n int) {
	s.db.SetMaxOpenConns(n)
}

func (s *SQLStore) SetMaxIdleConns(n int) {
	s.db.SetMaxIdleConns(n)
}

func (s *SQLStore) Close() {
	if s.db != nil {
		_ = s.db.Close()
	}
}

func (s *SQLStore) WithTx(fn func(tx *sql.Tx) error) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	if err = fn(tx); err != nil {
		return err
	}

	return tx.Commit()
}

func (s *SQLStore) QueryRow(q string, fn func(*sql.Row) error, args ...any) error {
	row := s.db.QueryRow(q, args...)
	return fn(row)
}

func (s *SQLStore) QueryRows(q string, fn func(*sql.Rows) error, args ...any) error {
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return err
	}
	defer func() {
		_ = rows.Close()
	}()

	for rows.Next() {
		if err = fn(rows); err != nil {
			return err
		}
	}

	if err = rows.Err(); err != nil {
		return err
	}

	return nil
}
