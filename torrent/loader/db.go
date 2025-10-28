package loader

import (
	"path"

	"github.com/anacrolix/torrent/metainfo"
	"github.com/dgraph-io/badger/v3"
	dlog "github.com/distribyted/distribyted/log"
	"github.com/rs/zerolog/log"
)

var _ LoaderAdder = &DB{}

const routeRootKey = "/route/"
const metaRootKey = "/meta/"
const fileRootKey = "/file/"

type DB struct {
	db *badger.DB
}

func NewDB(path string) (*DB, error) {
	l := log.Logger.With().Str("component", "torrent-store").Logger()

	opts := badger.DefaultOptions(path).
		WithLogger(&dlog.Badger{L: l}).
		WithValueLogFileSize(1<<26 - 1)

	db, err := badger.Open(opts)
	if err != nil {
		return nil, err
	}

	err = db.RunValueLogGC(0.5)
	if err != nil && err != badger.ErrNoRewrite {
		return nil, err
	}

	return &DB{
		db: db,
	}, nil
}

func (l *DB) AddMagnet(r, m string) error {
	err := l.db.Update(func(txn *badger.Txn) error {
		spec, err := metainfo.ParseMagnetUri(m)
		if err != nil {
			return err
		}

		ih := spec.InfoHash.HexString()

		rp := path.Join(routeRootKey, ih, r)
		return txn.Set([]byte(rp), []byte(m))
	})

	if err != nil {
		return err
	}

	return l.db.Sync()
}

func (l *DB) RemoveFromHash(r, h string) (bool, error) {
	tx := l.db.NewTransaction(true)
	defer tx.Discard()

	var mh metainfo.Hash
	if err := mh.FromHexString(h); err != nil {
		return false, err
	}

	rp := path.Join(routeRootKey, h, r)
	err := tx.Delete([]byte(rp))
	if err == badger.ErrKeyNotFound {
		// treat as deleted so UI doesn't get stuck
		return true, tx.Commit()
	}
	if err != nil {
		return false, err
	}
	return true, tx.Commit()
}

func (l *DB) ListMagnets() (map[string][]string, error) {
	tx := l.db.NewTransaction(false)
	defer tx.Discard()

	it := tx.NewIterator(badger.DefaultIteratorOptions)
	defer it.Close()

	prefix := []byte(routeRootKey)
	out := make(map[string][]string)
	for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
		_, r := path.Split(string(it.Item().Key()))
		i := it.Item()
		if err := i.Value(func(v []byte) error {
			out[r] = append(out[r], string(v))
			return nil
		}); err != nil {
			return nil, err
		}
	}

	return out, nil
}

func (l *DB) ListTorrentPaths() (map[string][]string, error) {
	tx := l.db.NewTransaction(false)
	defer tx.Discard()

	it := tx.NewIterator(badger.DefaultIteratorOptions)
	defer it.Close()

	prefix := []byte(fileRootKey)
	out := make(map[string][]string)
	for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
		key := string(it.Item().Key())
		// key format: /file/<hash>/<route>
		// extract route
		_, route := path.Split(key)
		i := it.Item()
		if err := i.Value(func(v []byte) error {
			out[route] = append(out[route], string(v))
			return nil
		}); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// SetMeta stores JSON-encoded metadata by hash
func (l *DB) SetMeta(hash string, meta []byte) error {
	err := l.db.Update(func(txn *badger.Txn) error {
		return txn.Set([]byte(path.Join(metaRootKey, hash)), meta)
	})
	if err != nil {
		return err
	}
	return l.db.Sync()
}

func (l *DB) GetMeta(hash string) ([]byte, error) {
	var out []byte
	err := l.db.View(func(txn *badger.Txn) error {
		it, err := txn.Get([]byte(path.Join(metaRootKey, hash)))
		if err != nil {
			return err
		}
		return it.Value(func(v []byte) error {
			out = append(out, v...)
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// GetAllMeta returns a map of hash->raw JSON metadata
func (l *DB) GetAllMeta() (map[string][]byte, error) {
	tx := l.db.NewTransaction(false)
	defer tx.Discard()
	it := tx.NewIterator(badger.DefaultIteratorOptions)
	defer it.Close()
	out := make(map[string][]byte)
	prefix := []byte(metaRootKey)
	for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
		key := string(it.Item().Key())
		_, hash := path.Split(key)
		i := it.Item()
		if err := i.Value(func(v []byte) error {
			// copy value
			b := make([]byte, len(v))
			copy(b, v)
			out[hash] = b
			return nil
		}); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (l *DB) DeleteMeta(hash string) error {
	return l.db.Update(func(txn *badger.Txn) error {
		return txn.Delete([]byte(path.Join(metaRootKey, hash)))
	})
}

// AddTorrentFile stores an association of .torrent file for route and hash
func (l *DB) AddTorrentFile(route, hash, filePath string) error {
	err := l.db.Update(func(txn *badger.Txn) error {
		rp := path.Join(fileRootKey, hash, route)
		return txn.Set([]byte(rp), []byte(filePath))
	})
	if err != nil {
		return err
	}
	return l.db.Sync()
}

func (l *DB) RemoveTorrentFile(route, hash string) error {
	return l.db.Update(func(txn *badger.Txn) error {
		rp := path.Join(fileRootKey, hash, route)
		return txn.Delete([]byte(rp))
	})
}

// ListMagnetHashesByRoute returns route->[]hash based on magnet entries
func (l *DB) ListMagnetHashesByRoute() (map[string][]string, error) {
	tx := l.db.NewTransaction(false)
	defer tx.Discard()
	it := tx.NewIterator(badger.DefaultIteratorOptions)
	defer it.Close()
	out := make(map[string][]string)
	prefix := []byte(routeRootKey)
	for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
		key := string(it.Item().Key())
		// key: /route/<hash>/<route>
		_, route := path.Split(key)
		parent := path.Dir(key)
		_, hash := path.Split(parent)
		out[route] = append(out[route], hash)
	}
	return out, nil
}

// ListFileHashesByRoute returns route->[]hash based on file entries
func (l *DB) ListFileHashesByRoute() (map[string][]string, error) {
	tx := l.db.NewTransaction(false)
	defer tx.Discard()
	it := tx.NewIterator(badger.DefaultIteratorOptions)
	defer it.Close()
	out := make(map[string][]string)
	prefix := []byte(fileRootKey)
	for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
		key := string(it.Item().Key())
		// key: /file/<hash>/<route>
		_, route := path.Split(key)
		parent := path.Dir(key)
		_, hash := path.Split(parent)
		out[route] = append(out[route], hash)
	}
	return out, nil
}

func (l *DB) Close() error {
	return l.db.Close()
}
