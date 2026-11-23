package repositories

// EmailRepository is the only layer that touches PostgreSQL for email data.
// Use store.Conn(ctx), a context from QueryTimeout, and return MapError(err).
type EmailRepository interface {
}

type emailRepository struct {
	store *Store
}

func NewEmailRepository(store *Store) EmailRepository {
	return &emailRepository{store: store}
}
