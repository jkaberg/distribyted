package loader

type Loader interface {
	ListMagnets() (map[string][]string, error)
	ListTorrentPaths() (map[string][]string, error)
}

type LoaderAdder interface {
	Loader

	RemoveFromHash(r, h string) (bool, error)
	AddMagnet(r, m string) error
	// DB-backed metadata and file mapping helpers
	SetMeta(hash string, meta []byte) error
	GetMeta(hash string) ([]byte, error)
	GetAllMeta() (map[string][]byte, error)
	DeleteMeta(hash string) error

	// Track .torrent file associations while keeping .torrent files on disk
	AddTorrentFile(route, hash, filePath string) error
	RemoveTorrentFile(route, hash string) error
	// Efficient hash listing without parsing magnet URI or reading files
	ListMagnetHashesByRoute() (map[string][]string, error)
	ListFileHashesByRoute() (map[string][]string, error)
}
