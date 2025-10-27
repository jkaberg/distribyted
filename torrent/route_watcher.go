package torrent

import (
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/rs/zerolog/log"

	"github.com/distribyted/distribyted/config"
)

type RouteWatcher struct {
	route  string
	folder string
	w      *fsnotify.Watcher
	s      *Service

	eventsCount uint64
}

func NewRouteWatcher(s *Service, route, folder string) (*RouteWatcher, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	return &RouteWatcher{
		route:  route,
		folder: folder,
		w:      w,
		s:      s,
	}, nil
}

func (rw *RouteWatcher) Start() error {
	if err := os.MkdirAll(rw.folder, 0744); err != nil {
		return err
	}

	// Add all existing subdirectories
	if err := filepath.Walk(rw.folder, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.Mode().IsDir() {
			return rw.w.Add(p)
		}
		return nil
	}); err != nil {
		return err
	}

	// Initial sync
	if err := rw.s.SyncRouteFolder(rw.route, rw.folder); err != nil {
		log.Error().Err(err).Str("route", rw.route).Str("folder", rw.folder).Msg("error syncing route folder on start")
	}

	go func() {
		for {
			select {
			case event, ok := <-rw.w.Events:
				if !ok {
					return
				}
				// Add newly created directories to watcher
				if event.Op&fsnotify.Create == fsnotify.Create {
					fi, err := os.Stat(event.Name)
					if err == nil && fi.IsDir() {
						_ = rw.w.Add(event.Name)
					}
				}
				atomic.AddUint64(&rw.eventsCount, 1)
			case err, ok := <-rw.w.Errors:
				if !ok {
					return
				}
				log.Error().Err(err).Str("route", rw.route).Str("folder", rw.folder).Msg("watcher error")
			}
		}
	}()

	go func() {
		for range time.Tick(time.Duration(rw.s.WatchInterval()) * time.Second) {
			if rw.eventsCount == 0 {
				continue
			}
			ec := rw.eventsCount
			if err := rw.s.SyncRouteFolder(rw.route, rw.folder); err != nil {
				log.Error().Err(err).Str("route", rw.route).Str("folder", rw.folder).Msg("error syncing route folder")
			}
			atomic.AddUint64(&rw.eventsCount, ^uint64(ec-1))
		}
	}()

	log.Info().Str("route", rw.route).Str("folder", rw.folder).Msg("route watcher started")
	return nil
}

func (rw *RouteWatcher) Close() error {
	if rw.w == nil {
		return nil
	}
	return rw.w.Close()
}

// StartRouteWatchers starts fsnotify watchers for all routes with a torrent folder
// and returns them to be closed on shutdown.
func StartRouteWatchers(s *Service, routes []*config.Route) ([]*RouteWatcher, error) {
	var out []*RouteWatcher
	for _, r := range routes {
		if r.TorrentFolder == "" {
			continue
		}
		rw, err := NewRouteWatcher(s, r.Name, r.TorrentFolder)
		if err != nil {
			return nil, err
		}
		if err := rw.Start(); err != nil {
			return nil, err
		}
		out = append(out, rw)
	}
	return out, nil
}

// StartRouteWatchersFromRoot scans a metadata routes root and starts a watcher for
// each route directory found under it. Expected layout: <root>/<route>
func StartRouteWatchersFromRoot(s *Service, root string) ([]*RouteWatcher, error) {
	var out []*RouteWatcher
	// Walk only first level directories under root
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		route := e.Name()
		folder := filepath.Join(root, route)
		// Ensure the route folder exists
		if err := os.MkdirAll(folder, 0744); err != nil {
			return nil, err
		}
		rw, err := NewRouteWatcher(s, route, folder)
		if err != nil {
			return nil, err
		}
		if err := rw.Start(); err != nil {
			return nil, err
		}
		out = append(out, rw)
	}
	return out, nil
}
