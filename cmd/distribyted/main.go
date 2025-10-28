package main

import (
	"bufio"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"github.com/anacrolix/missinggo/v2/filecache"
	"github.com/anacrolix/torrent/storage"
	"github.com/rs/zerolog/log"
	"github.com/urfave/cli/v2"

	"github.com/distribyted/distribyted/config"
	"github.com/distribyted/distribyted/fs"
	"github.com/distribyted/distribyted/fuse"
	"github.com/distribyted/distribyted/http"
	dlog "github.com/distribyted/distribyted/log"
	"github.com/distribyted/distribyted/torrent"
	"github.com/distribyted/distribyted/torrent/loader"
	"github.com/distribyted/distribyted/webdav"
)

const (
	configFlag     = "config"
	fuseAllowOther = "fuse-allow-other"
	portFlag       = "http-port"
	webDAVPortFlag = "webdav-port"
)

func main() {
	app := &cli.App{
		Name:  "distribyted",
		Usage: "Torrent client with on-demand file downloading as a filesystem.",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    configFlag,
				Value:   "./distribyted-data/config/config.yaml",
				EnvVars: []string{"DISTRIBYTED_CONFIG"},
				Usage:   "YAML file containing distribyted configuration.",
			},
			&cli.IntFlag{
				Name:    portFlag,
				Value:   4444,
				EnvVars: []string{"DISTRIBYTED_HTTP_PORT"},
				Usage:   "HTTP port for web interface.",
			},
			&cli.IntFlag{
				Name:    webDAVPortFlag,
				Value:   36911,
				EnvVars: []string{"DISTRIBYTED_WEBDAV_PORT"},
				Usage:   "Port used for WebDAV interface.",
			},
			&cli.BoolFlag{
				Name:    fuseAllowOther,
				Value:   false,
				EnvVars: []string{"DISTRIBYTED_FUSE_ALLOW_OTHER"},
				Usage:   "Allow other users to access all fuse mountpoints. You need to add user_allow_other flag to /etc/fuse.conf file.",
			},
		},

		Action: func(c *cli.Context) error {
			err := load(c.String(configFlag), c.Int(portFlag), c.Int(webDAVPortFlag), c.Bool(fuseAllowOther))

			// stop program execution on errors to avoid flashing consoles
			if err != nil && runtime.GOOS == "windows" {
				log.Error().Err(err).Msg("problem starting application")
				fmt.Print("Press 'Enter' to continue...")
				bufio.NewReader(os.Stdin).ReadBytes('\n')
			}

			return err
		},

		HideHelpCommand: true,
	}

	if err := app.Run(os.Args); err != nil {
		log.Fatal().Err(err).Msg("problem starting application")
	}
}

func load(configPath string, port, webDAVPort int, fuseAllowOther bool) error {
	ch := config.NewHandler(configPath)

	conf, err := ch.Get()
	if err != nil {
		return fmt.Errorf("error loading configuration: %w", err)
	}

	dlog.Load(conf.Log)

	if err := os.MkdirAll(conf.Torrent.MetadataFolder, 0744); err != nil {
		return fmt.Errorf("error creating metadata folder: %w", err)
	}

	cf := filepath.Join(conf.Torrent.MetadataFolder, "cache")
	fc, err := filecache.NewCache(cf)
	if err != nil {
		return fmt.Errorf("error creating cache: %w", err)
	}

	st := storage.NewResourcePieces(fc.AsResourceProvider())

	// cache is not working with windows
	if runtime.GOOS == "windows" {
		st = storage.NewFile(cf)
	}

	fis, err := torrent.NewFileItemStore(filepath.Join(conf.Torrent.MetadataFolder, "items"), 2*time.Hour)
	if err != nil {
		return fmt.Errorf("error starting item store: %w", err)
	}

	id, err := torrent.GetOrCreatePeerID(filepath.Join(conf.Torrent.MetadataFolder, "ID"))
	if err != nil {
		return fmt.Errorf("error creating node ID: %w", err)
	}

	c, dlLimiter, ulLimiter, err := torrent.NewClient(st, fis, conf.Torrent, id)
	if err != nil {
		return fmt.Errorf("error starting torrent client: %w", err)
	}

	cl := loader.NewConfig(conf.Routes)
	fl := loader.NewFolder(conf.Routes)
	ss := torrent.NewStats()

	dbl, err := loader.NewDB(filepath.Join(conf.Torrent.MetadataFolder, "magnetdb"))
	if err != nil {
		return fmt.Errorf("error starting magnet database: %w", err)
	}

	// UI-managed routesRoot: <metadata>/routes
	routesRoot := filepath.Join(conf.Torrent.MetadataFolder, "routes")
	if err := os.MkdirAll(routesRoot, 0744); err != nil {
		return fmt.Errorf("error creating routes root: %w", err)
	}

	ts := torrent.NewService([]loader.Loader{cl, fl}, dbl, ss, c,
		conf.Torrent.AddTimeout,
		conf.Torrent.ReadTimeout,
		conf.Torrent.ContinueWhenAddTimeout,
		routesRoot,
	)
	// store limiters for runtime settings and apply from config
	ts.SetLimiters(dlLimiter, ulLimiter)
	_ = ts.SetLimits(conf.Torrent.DownloadLimitMbit, conf.Torrent.UploadLimitMbit)

	var mh *fuse.Handler
	if conf.Fuse != nil {
		mh = fuse.NewHandler(fuseAllowOther || conf.Fuse.AllowOther, conf.Fuse.Path)
	}

	sigChan := make(chan os.Signal)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	log.Info().Msg(fmt.Sprintf("setting cache size to %d MB", conf.Torrent.GlobalCacheSize))
	fc.SetCapacity(conf.Torrent.GlobalCacheSize * 1024 * 1024)

	// Create an empty container FS and attach it before loading torrents so UI can start immediately
	empty := make(map[string]fs.Filesystem)
	cfs, err := fs.NewContainerFs(empty)
	if err != nil {
		return fmt.Errorf("error creating container fs: %w", err)
	}
	ts.SetContainerFs(cfs)
	// Give service config handler early
	ts.SetConfigHandler(ch)

	// Preload UI metadata from DB (fast startup) and start periodic DB persistence
	ts.LoadMetaFromDB()
	ts.StartMetaPersistence()

	// Pre-mount routes so FUSE/WebDAV/HTTPFS expose paths immediately
	ts.PreAddRoutes()
	// Load torrents and start watchers asynchronously to avoid delaying startup
	var watchers, uiWatchers []*torrent.RouteWatcher
	go func() {
		log.Info().Msg("loading torrents in background...")
		if _, e := ts.Load(); e != nil {
			log.Error().Err(e).Msg("error when loading torrents")
		}
		// Start route watchers for dynamic loading from configured torrent folders
		var wErr error
		watchers, wErr = torrent.StartRouteWatchers(ts, conf.Routes)
		if wErr != nil {
			log.Error().Err(wErr).Msg("error starting route watchers")
		}
		// Also start watchers for UI-managed routes under routesRoot
		uiWatchers, wErr = torrent.StartRouteWatchersFromRoot(ts, routesRoot)
		if wErr != nil {
			log.Error().Err(wErr).Msg("error starting UI route watchers")
		}
	}()

	httpfs := torrent.NewHTTPFS(cfs)
	logFilename := filepath.Join(conf.Log.Path, dlog.FileName)

	go func() {
		<-sigChan
		log.Info().Msg("closing route watchers...")
		for _, w := range watchers {
			if err := w.Close(); err != nil {
				log.Warn().Err(err).Msg("problem closing route watcher")
			}
		}
		for _, w := range uiWatchers {
			if err := w.Close(); err != nil {
				log.Warn().Err(err).Msg("problem closing route watcher")
			}
		}
		// stop periodic DB persistence and flush
		ts.StopMetaPersistence()
		log.Info().Msg("closing items database...")
		fis.Close()
		log.Info().Msg("closing magnet database...")
		dbl.Close()
		log.Info().Msg("closing torrent client...")
		c.Close()
		if mh != nil {
			log.Info().Msg("unmounting fuse filesystem...")
			mh.Unmount()
		}

		log.Info().Msg("exiting")
		os.Exit(1)
	}()

	go func() {
		if mh == nil {
			return
		}

		if err := mh.Mount(cfs); err != nil {
			log.Info().Err(err).Msg("error mounting filesystems")
		}
	}()

	go func() {
		if conf.WebDAV != nil {
			port = webDAVPort
			if port == 0 {
				port = conf.WebDAV.Port
			}

			if err := webdav.NewWebDAVServer(cfs, port, conf.WebDAV.User, conf.WebDAV.Pass); err != nil {
				log.Error().Err(err).Msg("error starting webDAV")
			}
		}

		log.Warn().Msg("webDAV configuration not found!")
	}()

	// Start health monitor if enabled
	ts.StartHealthMonitor(conf.Health)

	// removed: initial state dump

	err = http.New(fc, ss, ts, ch, httpfs, logFilename, conf.HTTPGlobal)
	log.Error().Err(err).Msg("error initializing HTTP server")
	return err
}
