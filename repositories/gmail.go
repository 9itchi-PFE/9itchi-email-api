package repositories

// GmailRepository is the only layer that touches PostgreSQL for gmail data.
// Use store.Conn(ctx), a context from QueryTimeout, and return MapError(err).
type GmailRepository interface {
}

type gmailRepository struct {
	store *Store
}

func NewGmailRepository(store *Store) GmailRepository {
	return &gmailRepository{store: store}
}
