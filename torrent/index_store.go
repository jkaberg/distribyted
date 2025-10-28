package torrent

import (
	"github.com/jkaberg/distribyted/torrent/loader"
)

// IndexStore abstracts persistent torrent index operations used by the service.
type IndexStore interface {
	// Route/torrent listing
	ListMagnets() (map[string][]string, error)
	ListTorrentPaths() (map[string][]string, error)

	// Mutations
	AddMagnet(route, magnet string) error
	RemoveFromHash(route, hash string) (bool, error)

	// Metadata cache
	SetMeta(hash string, meta []byte) error
	GetMeta(hash string) ([]byte, error)
	GetAllMeta() (map[string][]byte, error)
	DeleteMeta(hash string) error

	// .torrent file associations
	AddTorrentFile(route, hash, filePath string) error
	RemoveTorrentFile(route, hash string) error

	// Fast hash listing
	ListMagnetHashesByRoute() (map[string][]string, error)
	ListFileHashesByRoute() (map[string][]string, error)
}

// indexFromLoader adapts the existing loader.DB to IndexStore.
type indexFromLoader struct{ loader.LoaderAdder }

func NewIndexFromLoader(l loader.LoaderAdder) IndexStore { return &indexFromLoader{LoaderAdder: l} }
