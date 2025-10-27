package torrent

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	iofs "io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"golang.org/x/time/rate"

	cfgpkg "github.com/distribyted/distribyted/config"
	"github.com/distribyted/distribyted/fs"
	"github.com/distribyted/distribyted/torrent/loader"
)

type Service struct {
	c *torrent.Client

	s *Stats

	mu  sync.Mutex
	fss map[string]fs.Filesystem

	loaders []loader.Loader
	db      loader.LoaderAdder

	log                     zerolog.Logger
	addTimeout, readTimeout int
	continueWhenAddTimeout  bool

	readerPoolSize int
	readaheadMB    int

	// pathToHash keeps the association between a .torrent file path and the
	// corresponding torrent info hash for dynamic folder watching.
	pathToHash map[string]string

	// watchIntervalSec is the debounce interval in seconds used by folder watchers.
	watchIntervalSec int

	// routesRoot is the base directory where UI-managed routes are stored as
	// <routesRoot>/<route> containing .torrent files.
	routesRoot string

	// watchers holds active fsnotify watchers per route for UI-managed routes.
	watchers map[string]*RouteWatcher

	// cfs is the container filesystem used by HTTPFS/WebDAV. We add mounts
	// here so new routes appear immediately without restart.
	cfs *fs.ContainerFs

	// rate limiters
	dl *rate.Limiter
	ul *rate.Limiter

	// config handler for persistence
	ch *cfgpkg.Handler

	// health monitor
	hm *HealthMonitor

	// network status cache
	netMu        sync.Mutex
	cachedIP     string
	cachedConn   bool
	lastNetCheck time.Time

	// stop channel for background persistence
	persistStop chan struct{}

	// cached torrent state loaded from disk to avoid early network usage
	cached map[string]*cachedState
}

func NewService(loaders []loader.Loader, db loader.LoaderAdder, stats *Stats, c *torrent.Client, addTimeout, readTimeout int, continueWhenAddTimeout bool, routesRoot string) *Service {
	l := log.Logger.With().Str("component", "torrent-service").Logger()
	return &Service{
		log:                    l,
		s:                      stats,
		c:                      c,
		fss:                    make(map[string]fs.Filesystem),
		loaders:                loaders,
		db:                     db,
		addTimeout:             addTimeout,
		readTimeout:            readTimeout,
		continueWhenAddTimeout: continueWhenAddTimeout,
		pathToHash:             make(map[string]string),
		watchIntervalSec:       5,
		routesRoot:             routesRoot,
		watchers:               make(map[string]*RouteWatcher),
		readerPoolSize:         4,
		readaheadMB:            2,
		persistStop:            make(chan struct{}),
		cached:                 make(map[string]*cachedState),
	}
}

// SetContainerFs sets the container FS so new routes can be mounted dynamically.
func (s *Service) SetContainerFs(cfs *fs.ContainerFs) {
	s.mu.Lock()
	s.cfs = cfs
	s.mu.Unlock()
}

// SetLimiters stores the client limiters for runtime updates
func (s *Service) SetLimiters(dl, ul *rate.Limiter) {
	s.mu.Lock()
	s.dl = dl
	s.ul = ul
	s.mu.Unlock()
}

// SaveLimitsToConfig persists current limits to the YAML config via handler
func (s *Service) SaveLimitsToConfig(dlMbit, ulMbit float64) error {
	s.mu.Lock()
	ch := s.ch
	s.mu.Unlock()
	if ch == nil {
		return nil
	}
	conf, err := ch.Get()
	if err != nil {
		return err
	}
	if conf.Torrent == nil {
		conf.Torrent = &cfgpkg.TorrentGlobal{}
	}
	conf.Torrent.DownloadLimitMbit = dlMbit
	conf.Torrent.UploadLimitMbit = ulMbit
	return ch.Save(conf)
}

// SaveQbtToConfig persists the qBittorrent API enabled flag into the YAML config
func (s *Service) SaveQbtToConfig(enabled bool) error {
	s.mu.Lock()
	ch := s.ch
	s.mu.Unlock()
	if ch == nil {
		return nil
	}
	conf, err := ch.Get()
	if err != nil {
		return err
	}
	if conf.HTTPGlobal == nil {
		conf.HTTPGlobal = &cfgpkg.HTTPGlobal{}
	}
	conf.HTTPGlobal.QbittorrentAPI = enabled
	return ch.Save(conf)
}

// GetFuseBasePath returns the configured Fuse mount path or "/" if unavailable
func (s *Service) GetFuseBasePath() string {
	s.mu.Lock()
	ch := s.ch
	s.mu.Unlock()
	if ch == nil {
		return "/"
	}
	conf, err := ch.Get()
	if err != nil || conf.Fuse == nil || conf.Fuse.Path == "" {
		return "/"
	}
	return conf.Fuse.Path
}

// GetLimits returns current limits as Mbit/s
func (s *Service) GetLimits() (float64, float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	toMbit := func(l *rate.Limiter) float64 {
		if l == nil || l.Limit() == rate.Inf {
			return 0
		}
		// l.Limit() is tokens/sec where token=1 byte; convert to Mbit/s
		return float64(l.Limit()) * 8 / 1_000_000
	}
	return toMbit(s.dl), toMbit(s.ul)
}

// SetLimits updates limits from Mbit/s (0=unlimited)
func (s *Service) SetLimits(dlMbit, ulMbit float64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	set := func(l **rate.Limiter, mbit float64) {
		if mbit <= 0 {
			if *l == nil {
				*l = rate.NewLimiter(rate.Inf, 0)
			} else {
				(*l).SetLimit(rate.Inf)
			}
			return
		}
		bps := rate.Limit(mbit * 125_000) // bytes per second
		if *l == nil {
			*l = rate.NewLimiter(bps, int(bps))
		} else {
			(*l).SetLimit(bps)
			(*l).SetBurst(int(bps))
		}
	}
	set(&s.dl, dlMbit)
	set(&s.ul, ulMbit)
	// anacrolix/torrent uses ClientConfig rate limiters; at runtime we update
	// limiters stored in service which are referenced by the client.
	return nil
}

func (s *Service) Load() (map[string]fs.Filesystem, error) {
	// Load from config
	s.log.Info().Msg("adding torrents from configuration")
	for _, loader := range s.loaders {
		if err := s.load(loader); err != nil {
			return nil, err
		}
	}

	// Load from DB
	s.log.Info().Msg("adding torrents from database")
	return s.fss, s.load(s.db)
}

func (s *Service) load(l loader.Loader) error {
	list, err := l.ListMagnets()
	if err != nil {
		return err
	}
	for r, ms := range list {
		s.addRoute(r)
		for _, m := range ms {
			if err := s.addMagnet(r, m); err != nil {
				return err
			}
		}
	}

	list, err = l.ListTorrentPaths()
	if err != nil {
		return err
	}
	for r, ms := range list {
		s.addRoute(r)
		for _, p := range ms {
			if err := s.addTorrentPath(r, p); err != nil {
				return err
			}
		}
	}

	return nil
}

func (s *Service) AddMagnet(r, m string) error {
	if err := s.addMagnet(r, m); err != nil {
		return err
	}

	// Add to db
	return s.db.AddMagnet(r, m)
}

func (s *Service) addTorrentPath(r, p string) error {
	// Add to client
	t, err := s.c.AddTorrentFromFile(p)
	if err != nil {
		return err
	}

	return s.addTorrent(r, t)
}

// AddTorrentPath adds a torrent from a .torrent file into the given route and
// returns the new torrent hash. This does not persist anything into the DB.
func (s *Service) AddTorrentPath(r, p string) (string, error) {
	// Ensure route exists
	s.addRoute(r)

	// Add to client
	t, err := s.c.AddTorrentFromFile(p)
	if err != nil {
		return "", err
	}

	if err := s.addTorrent(r, t); err != nil {
		return "", err
	}

	h := t.InfoHash().HexString()

	s.mu.Lock()
	s.pathToHash[p] = h
	s.mu.Unlock()

	return h, nil
}

func (s *Service) addMagnet(r, m string) error {
	// Optionally augment magnet with extra trackers from config
	if aug, ok := s.augmentMagnetWithTrackers(m); ok {
		m = aug
	}
	// Add to client
	t, err := s.c.AddMagnet(m)
	if err != nil {
		return err
	}

	return s.addTorrent(r, t)

}

// augmentMagnetWithTrackers merges extra trackers from config into a magnet URI.
// Returns (magnet, true) if changed.
func (s *Service) augmentMagnetWithTrackers(m string) (string, bool) {
	s.mu.Lock()
	ch := s.ch
	s.mu.Unlock()
	if ch == nil {
		return m, false
	}
	conf, err := ch.Get()
	if err != nil || conf == nil || conf.Torrent == nil {
		return m, false
	}
	extra := make([]string, 0)
	// load from URL if present (best-effort, short timeout)
	if conf.Torrent.ExtraTrackersURL != "" {
		httpc := &http.Client{Timeout: 3 * time.Second}
		if resp, err := httpc.Get(conf.Torrent.ExtraTrackersURL); err == nil {
			if body, e := io.ReadAll(resp.Body); e == nil {
				for _, line := range strings.Split(string(body), "\n") {
					line = strings.TrimSpace(line)
					if line != "" && !strings.HasPrefix(line, "#") {
						extra = append(extra, line)
					}
				}
			}
			_ = resp.Body.Close()
		}
	}
	// merge static
	extra = append(extra, conf.Torrent.ExtraTrackers...)
	if len(extra) == 0 {
		return m, false
	}
	// parse magnet and append trackers
	u, err := url.Parse(m)
	if err != nil {
		return m, false
	}
	q := u.Query()
	// build set of existing trackers for dedupe
	have := make(map[string]struct{})
	for _, tr := range q["tr"] {
		have[tr] = struct{}{}
	}
	changed := false
	for _, tr := range extra {
		if _, ok := have[tr]; ok {
			continue
		}
		q.Add("tr", tr)
		changed = true
	}
	if !changed {
		return m, false
	}
	u.RawQuery = q.Encode()
	return u.String(), true
}

func (s *Service) addRoute(r string) {
	s.s.AddRoute(r)

	// Add to filesystems
	folder := path.Join("/", r)
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.fss[folder]
	if !ok {
		tfs := fs.NewTorrent(s.readTimeout)
		tfs.SetReaderPoolSize(s.readerPoolSize)
		tfs.SetReadaheadBytes(int64(s.readaheadMB) * 1024 * 1024)
		s.fss[folder] = tfs
		if s.cfs != nil {
			_ = s.cfs.AddFS(s.fss[folder], folder)
		}
	}
}

func (s *Service) addTorrent(r string, t *torrent.Torrent) error {
	// Only block on metadata when configured to do so. Otherwise, don't delay callers.
	if t.Info() == nil {
		if s.continueWhenAddTimeout {
			// Non-blocking: log when info arrives or times out
			go func(th string) {
				select {
				case <-t.GotInfo():
					s.log.Info().Str("hash", th).Msg("obtained torrent info")
				case <-time.After(time.Duration(s.addTimeout) * time.Second):
					s.log.Warn().Str("hash", th).Msg("timeout getting torrent info (non-blocking mode)")
				}
			}(t.InfoHash().String())
		} else {
			s.log.Info().Str("hash", t.InfoHash().String()).Msg("getting torrent info")
			select {
			case <-time.After(time.Duration(s.addTimeout) * time.Second):
				s.log.Warn().Str("hash", t.InfoHash().String()).Msg("timeout getting torrent info")
				return errors.New("timeout getting torrent info")
			case <-t.GotInfo():
				s.log.Info().Str("hash", t.InfoHash().String()).Msg("obtained torrent info")
			}
		}
	}

	// Add to stats immediately so UI can reflect it; piece/file loading is lazy in fs layer
	s.s.Add(r, t)

	// Add to filesystems
	folder := path.Join("/", r)
	s.mu.Lock()
	defer s.mu.Unlock()

	tfs, ok := s.fss[folder].(*fs.Torrent)
	if !ok {
		return errors.New("error adding torrent to filesystem")
	}

	tfs.AddTorrent(t)
	s.log.Info().Str("name", t.Info().Name).Str("route", r).Msg("torrent added")

	return nil
}

func (s *Service) RemoveFromHash(r, h string) error {
	// Remove from db
	deleted, err := s.db.RemoveFromHash(r, h)
	if err != nil {
		return err
	}

	if !deleted {
		return fmt.Errorf("element with hash %v on route %v cannot be removed", h, r)
	}

	// Remove from stats
	s.s.Del(r, h)

	// Remove from fs
	folder := path.Join("/", r)

	tfs, ok := s.fss[folder].(*fs.Torrent)
	if !ok {
		return errors.New("error removing torrent from filesystem")
	}

	tfs.RemoveTorrent(h)

	// Remove from client
	var mh metainfo.Hash
	if err := mh.FromHexString(h); err != nil {
		return err
	}

	t, ok := s.c.Torrent(metainfo.NewHashFromHex(h))
	if ok {
		t.Drop()
	}

	return nil
}

// RemoveFromHashLocal removes a torrent from runtime structures and client
// without touching the DB. Intended for file-based torrents added via watchers.
func (s *Service) RemoveFromHashLocal(r, h string) error {
	// Remove from stats
	s.s.Del(r, h)

	// Remove from fs
	folder := path.Join("/", r)

	tfs, ok := s.fss[folder].(*fs.Torrent)
	if !ok {
		return errors.New("error removing torrent from filesystem")
	}

	tfs.RemoveTorrent(h)

	// Remove from client
	var mh metainfo.Hash
	if err := mh.FromHexString(h); err != nil {
		return err
	}

	t, ok := s.c.Torrent(metainfo.NewHashFromHex(h))
	if ok {
		t.Drop()
	}

	return nil
}

// WatchInterval returns the current debounce interval in seconds used by the
// folder watchers.
func (s *Service) WatchInterval() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.watchIntervalSec
}

// SetWatchInterval updates the debounce interval used by the folder watchers.
func (s *Service) SetWatchInterval(interval int) {
	if interval <= 0 {
		return
	}
	s.mu.Lock()
	s.watchIntervalSec = interval
	s.mu.Unlock()
}

// SyncRouteFolder reconciles torrents in a route's folder with the current runtime state.
// It loads new .torrent files and unloads torrents whose files were removed.
func (s *Service) SyncRouteFolder(route, folder string) error {
	// Build set of files on disk
	disk := make(map[string]struct{})
	walkErr := filepath.WalkDir(folder, func(p string, d iofs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if filepath.Ext(p) == ".torrent" {
			disk[p] = struct{}{}
		}
		return nil
	})
	if walkErr != nil {
		return walkErr
	}

	// Current known files for this folder
	s.mu.Lock()
	current := make(map[string]string)
	for p, h := range s.pathToHash {
		if strings.HasPrefix(p, folder) {
			current[p] = h
		}
	}
	s.mu.Unlock()

	// Add new files
	for p := range disk {
		if _, ok := current[p]; !ok {
			if h, err := s.AddTorrentPath(route, p); err != nil {
				s.log.Error().Err(err).Str("path", p).Str("route", route).Msg("error adding torrent from file")
			} else {
				s.log.Info().Str("path", p).Str("hash", h).Str("route", route).Msg("torrent file added")
			}
		}
	}

	// Remove files missing from disk
	for p, h := range current {
		if _, ok := disk[p]; !ok {
			if err := s.RemoveFromHashLocal(route, h); err != nil {
				s.log.Error().Err(err).Str("path", p).Str("hash", h).Str("route", route).Msg("error removing torrent from missing file")
			} else {
				s.mu.Lock()
				delete(s.pathToHash, p)
				s.mu.Unlock()
				s.log.Info().Str("path", p).Str("hash", h).Str("route", route).Msg("torrent file removed")
			}
		}
	}

	return nil
}

// MaybeRemoveByPath removes a torrent by its .torrent file path if tracked.
func (s *Service) MaybeRemoveByPath(route, p string) bool {
	s.mu.Lock()
	h, ok := s.pathToHash[p]
	s.mu.Unlock()
	if !ok {
		return false
	}
	if err := s.RemoveFromHashLocal(route, h); err != nil {
		s.log.Warn().Err(err).Str("path", p).Str("route", route).Msg("error removing torrent by path")
		return false
	}
	s.mu.Lock()
	delete(s.pathToHash, p)
	s.mu.Unlock()
	return true
}

// EnsureRouteFolder ensures the UI-managed route folder exists and returns its path.
func (s *Service) EnsureRouteFolder(route string) (string, error) {
	if s.routesRoot == "" {
		return "", fmt.Errorf("routes root not configured")
	}
	folder := filepath.Join(s.routesRoot, route)
	if err := os.MkdirAll(folder, 0744); err != nil {
		return "", err
	}
	return folder, nil
}

// StartWatcherForRoute starts a watcher for the UI-managed route if not already running.
func (s *Service) StartWatcherForRoute(route string) error {
	folder, err := s.EnsureRouteFolder(route)
	if err != nil {
		return err
	}
	s.mu.Lock()
	if _, ok := s.watchers[route]; ok {
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()

	rw, err := NewRouteWatcher(s, route, folder)
	if err != nil {
		return err
	}
	if err := rw.Start(); err != nil {
		return err
	}
	s.mu.Lock()
	s.watchers[route] = rw
	s.mu.Unlock()
	return nil
}

// CreateRoute creates a new route and starts its watcher.
func (s *Service) CreateRoute(route string) error {
	if route == "" {
		return fmt.Errorf("route name required")
	}
	s.addRoute(route)
	if _, err := s.EnsureRouteFolder(route); err != nil {
		return err
	}
	// Mount into container FS so WebDAV/HTTPFS lists it
	folder := path.Join("/", route)
	s.mu.Lock()
	tfs := s.fss[folder]
	cfs := s.cfs
	s.mu.Unlock()
	if cfs != nil && tfs != nil {
		_ = cfs.AddFS(tfs, folder)
	}
	return s.StartWatcherForRoute(route)
}

// DeleteRoute removes all torrents associated to the route and deletes its UI folder.
func (s *Service) DeleteRoute(route string) error {
	if route == "" {
		return fmt.Errorf("route name required")
	}
	// Stop watcher if present
	s.mu.Lock()
	if rw, ok := s.watchers[route]; ok {
		_ = rw.Close()
		delete(s.watchers, route)
	}
	s.mu.Unlock()

	// Collect hashes to remove
	s.s.mut.Lock()
	var hashes []string
	if m, ok := s.s.torrentsByRoute[route]; ok {
		for h := range m {
			hashes = append(hashes, h)
		}
	}
	s.s.mut.Unlock()

	// Remove each torrent (also from DB if present)
	for _, h := range hashes {
		if err := s.RemoveFromHash(route, h); err != nil {
			// fallback to local removal if not in DB
			if err := s.RemoveFromHashLocal(route, h); err != nil {
				s.log.Warn().Str("route", route).Str("hash", h).Err(err).Msg("error removing torrent on route delete")
			}
		}
	}

	// Remove UI-managed folder
	if s.routesRoot != "" {
		folder := filepath.Join(s.routesRoot, route)
		if err := os.RemoveAll(folder); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	// Remove from stats so it disappears from UI list immediately
	s.s.RemoveRoute(route)

	// Unmount from container FS so it disappears from DAV/HTTPFS
	if s.cfs != nil {
		_ = s.cfs.RemoveFS(path.Join("/", route))
	}
	return nil
}

func (s *Service) SetConfigHandler(ch *cfgpkg.Handler) {
	s.mu.Lock()
	s.ch = ch
	s.mu.Unlock()
}

// StartStatePersistence periodically dumps torrent summaries to disk for fast startup
func (s *Service) StartStatePersistence() {
	s.mu.Lock()
	if s.persistStop == nil {
		s.persistStop = make(chan struct{})
	}
	stop := s.persistStop
	s.mu.Unlock()

	go func() {
		t := time.NewTicker(60 * time.Second)
		defer t.Stop()
		for {
			if err := s.dumpState(); err != nil {
				s.log.Warn().Err(err).Msg("state dump failed")
			}
			select {
			case <-t.C:
				continue
			case <-stop:
				return
			}
		}
	}()
}

// StopStatePersistence stops background persistence and performs a final dump
func (s *Service) StopStatePersistence() {
	s.mu.Lock()
	stop := s.persistStop
	s.persistStop = nil
	s.mu.Unlock()
	if stop != nil {
		close(stop)
	}
	_ = s.dumpState()
}

type summary struct {
	Hash       string        `json:"hash"`
	Route      string        `json:"route"`
	Name       string        `json:"name"`
	SizeBytes  int64         `json:"sizeBytes"`
	PieceBytes int64         `json:"pieceBytes"`
	AddedAt    int64         `json:"addedAt"`
	Peers      int           `json:"peers"`
	Seeders    int           `json:"seeders"`
	DownTotal  int64         `json:"downTotal"`
	UpTotal    int64         `json:"upTotal"`
	Files      []fileSummary `json:"files"`
}

type fileSummary struct {
	Path   string `json:"path"`
	Length int64  `json:"length"`
}

type cachedState struct {
	summary
}

func (s *Service) dumpState() error {
	// Build summaries without heavy per-piece data by inspecting current torrents and stats
	var out []summary
	// take snapshots under lock
	s.s.mut.Lock()
	for h, t := range s.s.torrents {
		route := s.s.RouteOf(h)
		var name string
		var size int64
		var piece int64
		var files []fileSummary
		if ti := t.Info(); ti != nil {
			name = t.Name()
			piece = ti.PieceLength
			for _, f := range ti.Files {
				files = append(files, fileSummary{Path: strings.Join(f.Path, string(os.PathSeparator)), Length: f.Length})
				size += f.Length
			}
			if size == 0 {
				size = ti.TotalLength()
			}
		} else if cs := s.cached[h]; cs != nil {
			name = cs.Name
			size = cs.SizeBytes
			piece = cs.PieceBytes
			files = cs.Files
		}
		prev := s.s.previousStats[h]
		var downTot, upTot int64
		var peers, seeders int
		var addedAt int64
		if prev != nil {
			downTot = prev.totalDownloadBytes
			upTot = prev.totalUploadBytes
			peers = prev.peers
			seeders = prev.seeders
			addedAt = prev.createdAt.Unix()
		}
		out = append(out, summary{
			Hash:       h,
			Route:      route,
			Name:       name,
			SizeBytes:  size,
			PieceBytes: piece,
			AddedAt:    addedAt,
			Peers:      peers,
			Seeders:    seeders,
			DownTotal:  downTot,
			UpTotal:    upTot,
			Files:      files,
		})
	}
	s.s.mut.Unlock()
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	// write to metadata folder
	s.mu.Lock()
	ch := s.ch
	s.mu.Unlock()
	if ch == nil {
		return nil
	}
	conf, err := ch.Get()
	if err != nil || conf == nil || conf.Torrent == nil {
		return err
	}
	path := filepath.Join(conf.Torrent.MetadataFolder, "state.json")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// LoadState pre-populates minimal torrent stats from disk so UI is instant
func (s *Service) LoadState() {
	s.mu.Lock()
	ch := s.ch
	s.mu.Unlock()
	if ch == nil {
		return
	}
	conf, err := ch.Get()
	if err != nil || conf == nil || conf.Torrent == nil {
		return
	}
	path := filepath.Join(conf.Torrent.MetadataFolder, "state.json")
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var list []summary
	if err := json.Unmarshal(b, &list); err != nil {
		return
	}
	// Pre-populate previousStats entries so listings have names immediately
	s.s.mut.Lock()
	now := time.Now()
	for _, sm := range list {
		if sm.Hash == "" {
			continue
		}
		s.s.previousStats[sm.Hash] = &stat{time: now, createdAt: time.Unix(sm.AddedAt, 0)}
		// cache full state for use before metadata arrives
		s.cached[sm.Hash] = &cachedState{summary: sm}
	}
	s.s.mut.Unlock()
}

// ConfigSnapshot returns a copy of the current config from handler, if present
func (s *Service) ConfigSnapshot() (*cfgpkg.Root, error) {
	s.mu.Lock()
	ch := s.ch
	s.mu.Unlock()
	if ch == nil {
		return nil, nil
	}
	return ch.Get()
}

// SaveHealthToConfig persists Health settings
func (s *Service) SaveHealthToConfig(h *cfgpkg.Health) error {
	s.mu.Lock()
	ch := s.ch
	s.mu.Unlock()
	if ch == nil {
		return nil
	}
	conf, err := ch.Get()
	if err != nil {
		return err
	}
	conf.Health = h
	return ch.Save(conf)
}

// SaveConfig loads the current config, applies the mutator, and saves it back.
func (s *Service) SaveConfig(mut func(*cfgpkg.Root)) error {
	s.mu.Lock()
	ch := s.ch
	s.mu.Unlock()
	if ch == nil {
		return nil
	}
	conf, err := ch.Get()
	if err != nil {
		return err
	}
	if conf == nil {
		conf = &cfgpkg.Root{}
	}
	mut(conf)
	return ch.Save(conf)
}

// StartHealthMonitor starts the background health monitor based on config
func (s *Service) StartHealthMonitor(conf *cfgpkg.Health) {
	if conf == nil || !conf.Enabled {
		return
	}
	if conf.IntervalMinutes < 60 {
		conf.IntervalMinutes = 60
	}
	s.mu.Lock()
	if s.hm != nil {
		s.hm.Stop()
		s.hm = nil
	}
	hm := &HealthMonitor{
		s:        s,
		interval: time.Duration(conf.IntervalMinutes) * time.Minute,
		grace:    time.Duration(conf.GraceMinutes) * time.Minute,
		minSeed:  conf.MinSeeders,
		httpc:    &http.Client{Timeout: 15 * time.Second},
		arr:      conf.Arr,
	}
	s.hm = hm
	s.mu.Unlock()
	go hm.run()
}

// StopHealthMonitor stops the monitor if running
func (s *Service) StopHealthMonitor() {
	s.mu.Lock()
	if s.hm != nil {
		s.hm.Stop()
		s.hm = nil
	}
	s.mu.Unlock()
}

// NetworkStatus attempts to determine a public IPv4 and whether we're connectible without external services.
// Strategy:
//   - Prefer configured `torrent.ip` if valid
//   - Else, use the anacrolix client PublicIp4 if available
//   - Connectible heuristic: if TCP is enabled and we have any active peers or listened before, assume true.
//     We also consider presence of a non-RFC1918 configured IP as connectible.
//
// Results are cached for 10s to avoid heavy calls.
func (s *Service) NetworkStatus() (string, bool) {
	s.netMu.Lock()
	if time.Since(s.lastNetCheck) < 10*time.Second {
		ip, conn := s.cachedIP, s.cachedConn
		s.netMu.Unlock()
		return ip, conn
	}
	s.netMu.Unlock()

	var pub string
	var connectible bool

	// 0) Try external IP services (pick based on time) to get public IP and basic reachability
	{
		services := []string{
			"https://api.ipify.org",
			"https://ifconfig.me/ip",
			"https://checkip.amazonaws.com",
		}
		idx := int(time.Now().UnixNano() % int64(len(services)))
		client := &http.Client{Timeout: 2 * time.Second}
		if resp, err := client.Get(services[idx]); err == nil {
			if body, e := io.ReadAll(resp.Body); e == nil {
				candidate := strings.TrimSpace(string(body))
				if ip := net.ParseIP(candidate); ip != nil {
					pub = ip.String()
					connectible = true // outbound to internet works
				}
			}
			_ = resp.Body.Close()
		}
	}

	// Snapshot config handler
	s.mu.Lock()
	ch := s.ch
	s.mu.Unlock()

	// 1) Configured IP wins
	if ch != nil {
		if conf, err := ch.Get(); err == nil && conf != nil && conf.Torrent != nil {
			if ip := net.ParseIP(conf.Torrent.IP); ip != nil {
				pub = ip.String()
			}
		}
	}

	// Note: we skip probing anacrolix client for PublicIp4 for compatibility across versions

	// 3) Heuristic connectible: if we have a public IP and not RFC1918, lean true
	if pub != "" {
		if ip := net.ParseIP(pub); ip != nil {
			// RFC1918 private ranges
			private := ip.IsPrivate()
			connectible = !private
		}
	}

	// 4) If unknown, infer from peers activity and recent upload
	if !connectible {
		// If any torrent reports seeders/peers recently, likely reachable
		rs := s.s.RoutesStats()
		for _, r := range rs {
			for _, t := range r.TorrentStats {
				if t.Peers > 0 || t.Seeders > 0 || t.UploadedBytes > 0 {
					connectible = true
					break
				}
			}
			if connectible {
				break
			}
		}
	}

	s.netMu.Lock()
	s.cachedIP = pub
	s.cachedConn = connectible
	s.lastNetCheck = time.Now()
	s.netMu.Unlock()

	return pub, connectible
}

// BlacklistAndRemove blacklists the torrent in Arr (if possible) and removes it locally
func (s *Service) BlacklistAndRemove(route, hash string) error {
	conf, _ := s.ConfigSnapshot()
	var arr []*cfgpkg.ArrInstance
	if conf != nil && conf.Health != nil {
		arr = conf.Health.Arr
	}
	// Try to resolve Arr clients for the given route via categories
	httpc := &http.Client{Timeout: 15 * time.Second}
	catToClients := resolveArrManagedRoutes(arr, httpc)
	for _, c := range catToClients[route] {
		if id, entity, entityID, err := c.findQueueByHash(hash); err == nil {
			_ = c.blacklistQueueItem(id)
			sleepShort()
			_ = c.triggerSearch(entity, entityID)
		}
	}
	// Remove from runtime (and DB if present)
	if err := s.RemoveFromHash(route, hash); err != nil {
		if err := s.RemoveFromHashLocal(route, hash); err != nil {
			return err
		}
	}
	return nil
}

// ApplyTorrentTuning updates runtime FS parameters and persists to config if handler is set.
func (s *Service) ApplyTorrentTuning(poolSize, readaheadMB int) error {
	if poolSize <= 0 {
		poolSize = 1
	}
	if readaheadMB < 0 {
		readaheadMB = 0
	}
	s.mu.Lock()
	s.readerPoolSize = poolSize
	s.readaheadMB = readaheadMB
	// update all existing FS instances
	for _, f := range s.fss {
		if tf, ok := f.(*fs.Torrent); ok {
			tf.SetReaderPoolSize(poolSize)
			tf.SetReadaheadBytes(int64(readaheadMB) * 1024 * 1024)
		}
	}
	ch := s.ch
	s.mu.Unlock()
	if ch != nil {
		return s.SaveConfig(func(conf *cfgpkg.Root) {
			if conf.Torrent == nil {
				conf.Torrent = &cfgpkg.TorrentGlobal{}
			}
			conf.Torrent.ReaderPoolSize = poolSize
			conf.Torrent.ReadaheadMB = readaheadMB
		})
	}
	return nil
}

// HealthMonitor periodically evaluates torrent health and interacts with Arr
type HealthMonitor struct {
	s        *Service
	stop     chan struct{}
	interval time.Duration
	grace    time.Duration
	minSeed  int
	httpc    *http.Client
	arr      []*cfgpkg.ArrInstance
}

func (hm *HealthMonitor) run() {
	hm.stop = make(chan struct{})
	t := time.NewTicker(hm.interval)
	defer t.Stop()
	for {
		hm.checkOnce()
		select {
		case <-t.C:
			continue
		case <-hm.stop:
			return
		}
	}
}

func (hm *HealthMonitor) Stop() { close(hm.stop) }

func (hm *HealthMonitor) checkOnce() {
	// Restrict to Arr-managed routes when possible
	catToClients := resolveArrManagedRoutes(hm.arr, hm.httpc)
	routes := hm.s.s.RoutesStats()
	now := time.Now()
	for _, rs := range routes {
		// if arr-managed categories configured, skip routes that don't match
		if len(catToClients) > 0 {
			if _, ok := catToClients[rs.Name]; !ok {
				continue
			}
		}
		for _, ts := range rs.TorrentStats {
			if ts.AddedAt > 0 && now.Sub(time.Unix(ts.AddedAt, 0)) < hm.grace {
				continue
			}
			// Unknown or zero seeders => unhealthy
			if ts.Seeders <= 0 {
				hm.handleUnhealthy(rs.Name, ts.Hash, catToClients[rs.Name])
				continue
			}
			if ts.Seeders < hm.minSeed {
				hm.handleUnhealthy(rs.Name, ts.Hash, catToClients[rs.Name])
			}
		}
	}
}

func (hm *HealthMonitor) handleUnhealthy(route, hash string, clients []*arrClient) {
	// Reuse shared service method to ensure consistent behavior with manual overrides
	_ = hm.s.BlacklistAndRemove(route, hash)
}
