package http

import (
	"fmt"
	"net/http"

	"github.com/anacrolix/missinggo/v2/filecache"
	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"
	"github.com/shurcooL/httpfs/html/vfstemplate"

	"github.com/distribyted/distribyted"
	"github.com/distribyted/distribyted/config"
	"github.com/distribyted/distribyted/torrent"
)

func New(fc *filecache.Cache, ss *torrent.Stats, s *torrent.Service, ch *config.Handler, fs http.FileSystem, logPath string, cfg *config.HTTPGlobal) error {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(gin.ErrorLogger())
	r.Use(Logger())

	r.GET("/assets/*filepath", func(c *gin.Context) {
		c.FileFromFS(c.Request.URL.Path, http.FS(distribyted.Assets))
	})

	if cfg.HTTPFS {
		log.Info().Str("host", fmt.Sprintf("%s:%d/fs", cfg.IP, cfg.Port)).Msg("starting HTTPFS")
		h := func(c *gin.Context) {
			path := c.Param("filepath")
			c.FileFromFS(path, fs)
		}
		r.GET("/fs/*filepath", h)
		r.HEAD("/fs/*filepath", h)

	}

	t, err := vfstemplate.ParseGlob(http.FS(distribyted.Templates), nil, "/templates/*")
	if err != nil {
		return fmt.Errorf("error parsing html: %w", err)
	}

	r.SetHTMLTemplate(t)
	// give service access to config handler for persistence
	s.SetConfigHandler(ch)

	r.GET("/", indexHandler(ss))
	r.GET("/routes", indexHandler(ss))
	r.GET("/dashboard", dashboardHandler)
	r.GET("/logs", logsHandler)
	r.GET("/settings", settingsHandler)

	api := r.Group("/api")
	{
		api.GET("/log", apiLogHandler(logPath))
		api.GET("/status", apiStatusHandler(fc, ss))
		api.GET("/net", apiNetHandler(s))

		api.GET("/routes", apiRoutesHandler(ss, s))
		api.GET("/routes/:route/torrents", apiRouteTorrentsHandler(ss))
		api.GET("/routes/:route/torrent/:torrent_hash", apiTorrentDetailsHandler(ss, s))
		api.POST("/routes", apiCreateRouteHandler(s))
		api.DELETE("/routes/:route", apiDeleteRouteHandler(s))
		api.GET("/routes/:route/files", apiListRouteFiles(s))
		api.POST("/routes/:route/files", apiUploadTorrent(s))
		api.DELETE("/routes/:route/files/:name", apiDeleteTorrentFile(s))

		api.POST("/routes/:route/torrent", apiAddTorrentHandler(s))
		api.DELETE("/routes/:route/torrent/:torrent_hash", apiDelTorrentHandler(s))
		api.POST("/routes/:route/torrent/:torrent_hash/blacklist", apiBlacklistTorrentHandler(s))

		// watcher interval endpoints
		api.GET("/watch_interval", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"interval": s.WatchInterval()})
		})
		api.POST("/watch_interval", func(c *gin.Context) {
			var req struct {
				Interval int `json:"interval"`
			}
			if err := c.ShouldBindJSON(&req); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}
			s.SetWatchInterval(req.Interval)
			c.JSON(http.StatusOK, gin.H{"interval": s.WatchInterval()})
		})

		// rate limit endpoints (Mbit/s)
		api.GET("/settings/limits", apiGetLimitsHandler(s))
		api.POST("/settings/limits", apiSetLimitsHandler(s))

		// qBittorrent API toggle endpoints
		api.GET("/settings/qbt", apiGetQbtHandler())
		api.POST("/settings/qbt", apiSetQbtHandler(s))

		// Health settings endpoints
		api.GET("/settings/health", apiGetHealthHandler(s))
		api.POST("/settings/health", apiSetHealthHandler(s))
		api.POST("/settings/health/arr/test", apiTestArrHandler())

		// General config endpoints
		api.GET("/settings/config", apiGetConfigHandler(s))
		api.POST("/settings/config", apiSetConfigHandler(s))

	}

	// qBittorrent-compatible API can be toggled at runtime; set initial state from config
	SetQbtEnabled(cfg.QbittorrentAPI)
	v2 := r.Group("/api/v2")
	{
		registerQBittorrentAPI(v2, ss, s)
	}

	log.Info().Str("host", fmt.Sprintf("%s:%d", cfg.IP, cfg.Port)).Msg("starting webserver")

	if err := r.Run(fmt.Sprintf("%s:%d", cfg.IP, cfg.Port)); err != nil {
		return fmt.Errorf("error initializing server: %w", err)
	}

	return nil
}

func Logger() gin.HandlerFunc {
	l := log.Logger.With().Str("component", "http").Logger()
	return func(c *gin.Context) {
		path := c.Request.URL.Path
		raw := c.Request.URL.RawQuery
		c.Next()
		if raw != "" {
			path = path + "?" + raw
		}
		msg := c.Errors.String()
		if msg == "" {
			msg = "Request"
		}

		s := c.Writer.Status()
		switch {
		case s >= 400 && s < 500:
			l.Warn().Str("path", path).Int("status", s).Msg(msg)
		case s >= 500:
			l.Error().Str("path", path).Int("status", s).Msg(msg)
		default:
			l.Debug().Str("path", path).Int("status", s).Msg(msg)
		}
	}
}
