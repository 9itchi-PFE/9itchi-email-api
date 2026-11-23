package repositories

// ApplicationRepository is the only layer that touches PostgreSQL for application data.
// Use store.Conn(ctx), a context from QueryTimeout, and return MapError(err).
type ApplicationRepository interface {
}

type applicationRepository struct {
	store *Store
}

func NewApplicationRepository(store *Store) ApplicationRepository {
	return &applicationRepository{store: store}
}
