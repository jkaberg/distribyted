package server

import (
	stdhttp "net/http"

	"github.com/anacrolix/missinggo/v2/filecache"

	"github.com/jkaberg/distribyted/config"
	"github.com/jkaberg/distribyted/fs"
	apphttp "github.com/jkaberg/distribyted/http"
	"github.com/jkaberg/distribyted/torrent"
	"github.com/jkaberg/distribyted/webdav"
	"github.com/rs/zerolog/log"
)

// StartServers starts Web UI (HTTP) and WebDAV (if configured).
// Returns when the HTTP server exits (it is blocking by design).
func StartServers(fc *filecache.Cache, cfs fs.Filesystem, httpConf *config.HTTPGlobal, webdavConf *config.WebDAVGlobal, stats *torrent.Stats, svc *torrent.Service, ch *config.Handler, httpfs stdhttp.FileSystem, logPath string) error {
	log.Info().Msg("starting servers")
	// WebDAV in background if configured
	if webdavConf != nil {
		go func() { _ = webdav.NewWebDAVServer(cfs, webdavConf.Port, webdavConf.User, webdavConf.Pass) }()
	}
	// Start HTTP server (blocking)
	return apphttp.New(fc, stats, svc, ch, httpfs, logPath, httpConf)
}
