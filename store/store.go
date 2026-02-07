package store

type Store struct {
	SQL *SQLStore
}

func Open(sqlFn func() (*SQLStore, error)) (*Store, error) {
	sqlStore, err := sqlFn()
	if err != nil {
		return nil, err
	}

	return &Store{
		SQL: sqlStore,
	}, nil
}

func (s *Store) Close() {
	if s.SQL != nil {
		s.SQL.Close()
	}
}
