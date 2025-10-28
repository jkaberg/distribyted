package http

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/anacrolix/missinggo/v2/filecache"
	cfgpkg "github.com/distribyted/distribyted/config"
	"github.com/distribyted/distribyted/torrent"
	"github.com/gin-gonic/gin"
)

var apiStatusHandler = func(fc *filecache.Cache, ss *torrent.Stats) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		// TODO move to a struct
		ctx.JSON(http.StatusOK, gin.H{
			"cacheItems":    fc.Info().NumItems,
			"cacheFilled":   fc.Info().Filled / 1024 / 1024,
			"cacheCapacity": fc.Info().Capacity / 1024 / 1024,
			"torrentStats":  ss.GlobalStats(),
		})
	}
}

// apiRoutesHandler returns route stats enriched with the on-disk folder path
var apiRoutesHandler = func(ss *torrent.Stats, svc *torrent.Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		// Lightweight summaries (no per-piece stats); if empty, use cached state
		sums := ss.RouteSummaries()
		if len(sums) == 0 {
			// synthesize summaries from cached engine state
			crs := svc.CachedRoutesStats()
			type routeOut struct {
				Name  string
				Total int
			}
			ro := make([]*routeOut, 0, len(crs))
			for _, r := range crs {
				ro = append(ro, &routeOut{Name: r.Name, Total: len(r.TorrentStats)})
			}
			// enrich with folders and return
			type outT struct {
				Name, Folder string
				Total        int
			}
			out := make([]*outT, 0, len(ro))
			for _, r := range ro {
				folder := ""
				if f, err := svc.EnsureRouteFolder(r.Name); err == nil {
					folder = f
				}
				out = append(out, &outT{Name: r.Name, Folder: folder, Total: r.Total})
			}
			ctx.JSON(http.StatusOK, out)
			return
		}
		// Merge cached counts with live: use max(live, cached) to avoid shrinking totals
		if len(sums) > 0 {
			cm := make(map[string]int)
			for _, r := range svc.CachedRoutesStats() {
				cm[r.Name] = len(r.TorrentStats)
			}
			type routeOut struct {
				Name   string `json:"name"`
				Folder string `json:"folder"`
				Total  int    `json:"total"`
			}
			out := make([]*routeOut, 0, len(sums))
			for _, r := range sums {
				folder := ""
				if f, err := svc.EnsureRouteFolder(r.Name); err == nil {
					folder = f
				}
				total := r.Total
				if ct, ok := cm[r.Name]; ok && ct > total {
					total = ct
				}
				out = append(out, &routeOut{Name: r.Name, Folder: folder, Total: total})
			}
			ctx.JSON(http.StatusOK, out)
			return
		}
		// fallback shouldn't reach here, but keep original path
		type routeOut struct {
			Name   string `json:"name"`
			Folder string `json:"folder"`
			Total  int    `json:"total"`
		}
		out := make([]*routeOut, 0, len(sums))
		for _, r := range sums {
			folder := ""
			if f, err := svc.EnsureRouteFolder(r.Name); err == nil {
				folder = f
			}
			out = append(out, &routeOut{Name: r.Name, Folder: folder, Total: r.Total})
		}
		ctx.JSON(http.StatusOK, out)
	}
}

var apiAddTorrentHandler = func(s *torrent.Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		route := ctx.Param("route")

		var json RouteAdd
		if err := ctx.ShouldBindJSON(&json); err != nil {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		if err := s.AddMagnet(route, json.Magnet); err != nil {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		ctx.JSON(http.StatusOK, nil)
	}
}

var apiDelTorrentHandler = func(s *torrent.Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		route := ctx.Param("route")
		hash := ctx.Param("torrent_hash")

		if err := s.RemoveFromHash(route, hash); err != nil {
			// Fallback to local removal (file-based torrents not in DB)
			if err := s.RemoveFromHashLocal(route, hash); err != nil {
				ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}
		}

		ctx.JSON(http.StatusOK, nil)
	}
}

// apiBlacklistTorrentHandler removes a torrent and triggers Arr blacklist/search if possible
var apiBlacklistTorrentHandler = func(s *torrent.Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		route := ctx.Param("route")
		hash := ctx.Param("torrent_hash")
		if err := s.BlacklistAndRemove(route, hash); err != nil {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		ctx.JSON(http.StatusOK, gin.H{"ok": true})
	}
}

// apiRouteTorrentsHandler returns paginated torrents for a route
var apiRouteTorrentsHandler = func(ss *torrent.Stats, svc *torrent.Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		route := ctx.Param("route")
		page := 1
		size := 25
		if p := ctx.Query("page"); p != "" {
			if v, err := strconv.Atoi(p); err == nil && v > 0 {
				page = v
			}
		}
		if s := ctx.Query("size"); s != "" {
			if v, err := strconv.Atoi(s); err == nil && v > 0 {
				size = v
			}
		}
		// Always serve a merged view so cached items persist until live torrents load
		rp := svc.MergedRoutePage(route, page, size)
		ctx.JSON(http.StatusOK, rp)
	}
}

// apiTorrentDetailsHandler returns detailed info for a torrent including common paths
var apiTorrentDetailsHandler = func(ss *torrent.Stats, svc *torrent.Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		route := ctx.Param("route")
		hash := ctx.Param("torrent_hash")

		ts, err := ss.Stats(hash)
		if err != nil {
			// Try cached stat to avoid 404 during startup
			if cs := svc.CachedStat(hash); cs != nil {
				ts = cs
			} else {
				ctx.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
				return
			}
		}

		folder, _ := svc.EnsureRouteFolder(route)
		httpfsPath := "/fs/" + route
		fusePath := ""
		if conf, err := svc.ConfigSnapshot(); err == nil && conf != nil && conf.Fuse != nil && conf.Fuse.Path != "" {
			fusePath = filepath.Join(conf.Fuse.Path, route)
		}

		ctx.JSON(http.StatusOK, gin.H{
			"route":  route,
			"hash":   hash,
			"stats":  ts,
			"folder": folder,
			"paths": gin.H{
				"httpfs": httpfsPath,
				"fuse":   fusePath,
			},
		})
	}
}

// apiTorrentFilesHandler returns the list of files for a torrent hash
var apiTorrentFilesHandler = func(ss *torrent.Stats, svc *torrent.Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		// route param is not strictly needed here, kept for consistency
		_ = ctx.Param("route")
		hash := ctx.Param("torrent_hash")
		files, err := svc.FilesForHash(hash)
		if err != nil {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		// files already contains { path, length }
		ctx.JSON(http.StatusOK, gin.H{"files": files})
	}
}

// apiCreateRouteHandler creates a new route (UI-managed)
var apiCreateRouteHandler = func(s *torrent.Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		var body RouteCreate
		if err := ctx.ShouldBindJSON(&body); err != nil {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if err := s.CreateRoute(body.Name); err != nil {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		ctx.JSON(http.StatusOK, gin.H{"name": body.Name})
	}
}

// apiDeleteRouteHandler deletes a route and its UI-managed folder
var apiDeleteRouteHandler = func(s *torrent.Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		route := ctx.Param("route")
		if err := s.DeleteRoute(route); err != nil {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		ctx.JSON(http.StatusOK, nil)
	}
}

// apiListRouteFiles lists .torrent files present in the UI-managed folder for a route
var apiListRouteFiles = func(s *torrent.Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		route := ctx.Param("route")
		folder, err := s.EnsureRouteFolder(route)
		if err != nil {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		entries, err := os.ReadDir(folder)
		if err != nil {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		var files []string
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			if filepath.Ext(e.Name()) == ".torrent" {
				files = append(files, e.Name())
			}
		}
		ctx.JSON(http.StatusOK, gin.H{"files": files})
	}
}

// apiUploadTorrent uploads a .torrent file into the UI-managed route folder and loads it
var apiUploadTorrent = func(s *torrent.Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		route := ctx.Param("route")

		// Limit to a single file named "file"
		fh, err := ctx.FormFile("file")
		if err != nil {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("upload: %v", err)})
			return
		}
		if filepath.Ext(fh.Filename) != ".torrent" {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": "only .torrent files allowed"})
			return
		}
		folder, err := s.EnsureRouteFolder(route)
		if err != nil {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		dst := filepath.Join(folder, filepath.Base(fh.Filename))
		if err := ctx.SaveUploadedFile(fh, dst); err != nil {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		// Load into runtime
		if _, err := s.AddTorrentPath(route, dst); err != nil {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		ctx.JSON(http.StatusOK, gin.H{"path": dst})
	}
}

// apiDeleteTorrentFile removes a .torrent file from the UI-managed folder and unloads it if loaded
var apiDeleteTorrentFile = func(s *torrent.Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		route := ctx.Param("route")
		name := ctx.Param("name")
		if filepath.Ext(name) != ".torrent" {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": "invalid file"})
			return
		}
		folder, err := s.EnsureRouteFolder(route)
		if err != nil {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		p := filepath.Join(folder, filepath.Base(name))
		// If loaded, remove from runtime first
		s.MaybeRemoveByPath(route, p)
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		ctx.JSON(http.StatusOK, nil)
	}
}

var apiLogHandler = func(path string) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		f, err := os.Open(path)
		if err != nil {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		fi, err := f.Stat()
		if err != nil {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		max := math.Max(float64(-fi.Size()), -1024*8*8)
		_, err = f.Seek(int64(max), io.SeekEnd)
		if err != nil {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		var b bytes.Buffer
		ctx.Stream(func(w io.Writer) bool {
			_, err := b.ReadFrom(f)
			if err != nil {
				ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return false
			}

			_, err = b.WriteTo(w)
			if err != nil {
				ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return false
			}

			return true
		})

		if err := f.Close(); err != nil {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
	}
}

// Limits payload in Mbit/s
type limitsPayload struct {
	DownloadMbit float64 `json:"downloadMbit"`
	UploadMbit   float64 `json:"uploadMbit"`
}

// apiGetLimitsHandler reads current rate limits (Mbit/s)
var apiGetLimitsHandler = func(s *torrent.Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		dl, ul := s.GetLimits()
		ctx.JSON(http.StatusOK, gin.H{
			"downloadMbit": dl,
			"uploadMbit":   ul,
		})
	}
}

// Health settings
type healthPayload struct {
	Enabled          bool                  `json:"enabled"`
	IntervalMinutes  int                   `json:"intervalMinutes"`
	GraceMinutes     int                   `json:"graceMinutes"`
	MinSeeders       int                   `json:"minSeeders"`
	GoodSeeders      int                   `json:"goodSeeders"`
	ExcellentSeeders int                   `json:"excellentSeeders"`
	Arr              []*cfgpkg.ArrInstance `json:"arr"`
}

var apiGetHealthHandler = func(s *torrent.Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		// load from config handler to reflect persisted state
		hp := healthPayload{}
		// default fallbacks
		hp.IntervalMinutes = 60
		hp.GraceMinutes = 30
		hp.MinSeeders = 2

		if err := func() error {
			// service holds config handler
			conf, err := s.ConfigSnapshot()
			if err != nil || conf == nil {
				return err
			}
			if conf.Health != nil {
				hp.Enabled = conf.Health.Enabled
				hp.IntervalMinutes = conf.Health.IntervalMinutes
				hp.GraceMinutes = conf.Health.GraceMinutes
				hp.MinSeeders = conf.Health.MinSeeders
				hp.GoodSeeders = conf.Health.GoodSeeders
				hp.ExcellentSeeders = conf.Health.ExcellentSeeders
				hp.Arr = conf.Health.Arr
			}
			return nil
		}(); err != nil {
			// ignore and return defaults
		}
		ctx.JSON(http.StatusOK, hp)
	}
}

var apiSetHealthHandler = func(s *torrent.Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		var body healthPayload
		if err := ctx.ShouldBindJSON(&body); err != nil {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		// persist to config
		if err := s.SaveHealthToConfig(&cfgpkg.Health{
			Enabled:          body.Enabled,
			IntervalMinutes:  body.IntervalMinutes,
			GraceMinutes:     body.GraceMinutes,
			MinSeeders:       body.MinSeeders,
			GoodSeeders:      body.GoodSeeders,
			ExcellentSeeders: body.ExcellentSeeders,
			Arr:              body.Arr,
		}); err != nil {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		ctx.JSON(http.StatusOK, gin.H{"ok": true})
	}
}

// apiTestArrHandler tests connectivity to a single Arr instance
func apiTestArrHandler() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		var inst cfgpkg.ArrInstance
		if err := ctx.ShouldBindJSON(&inst); err != nil {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if strings.TrimSpace(inst.BaseURL) == "" || strings.TrimSpace(inst.APIKey) == "" {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": "base_url and api_key required"})
			return
		}
		// Build status endpoint based on type
		endpoint := "/api/v3/system/status"
		if inst.Type == cfgpkg.ArrLidarr {
			endpoint = "/api/v1/system/status"
		}
		// Prepare client
		httpc := &http.Client{Timeout: 10 * time.Second}
		// Compose URL
		u, err := func() (string, error) {
			uu, e := url.Parse(inst.BaseURL)
			if e != nil {
				return "", e
			}
			uu.Path = path.Join(uu.Path, endpoint)
			return uu.String(), nil
		}()
		if err != nil {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		req, err := http.NewRequest(http.MethodGet, u, nil)
		if err != nil {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		req.Header.Set("X-Api-Key", inst.APIKey)
		resp, err := httpc.Do(req)
		if err != nil {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("HTTP %d", resp.StatusCode)})
			return
		}
		// minimal parse for name/version
		var obj struct {
			Version string `json:"version"`
			AppName string `json:"appName"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&obj)
		ctx.JSON(http.StatusOK, gin.H{"ok": true, "name": obj.AppName, "version": obj.Version})
	}
}

// apiNetHandler returns basic network info: public IP and whether we're likely connectible
var apiNetHandler = func(s *torrent.Service) gin.HandlerFunc {
	type netOut struct {
		PublicIP    string `json:"publicIp"`
		Connectible bool   `json:"connectible"`
	}
	return func(ctx *gin.Context) {
		ip, conn := s.NetworkStatus()
		ctx.JSON(http.StatusOK, netOut{PublicIP: ip, Connectible: conn})
	}
}

// General config get/set (excluding routes)
var apiGetConfigHandler = func(s *torrent.Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		conf, err := s.ConfigSnapshot()
		if err != nil {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if conf == nil {
			conf = &cfgpkg.Root{}
		}
		// Return only relevant sections
		ctx.JSON(http.StatusOK, gin.H{
			"http":    conf.HTTPGlobal,
			"webdav":  conf.WebDAV,
			"torrent": conf.Torrent,
			"fuse":    conf.Fuse,
			"log":     conf.Log,
		})
	}
}

var apiSetConfigHandler = func(s *torrent.Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		var body struct {
			HTTP    *cfgpkg.HTTPGlobal    `json:"http"`
			WebDAV  *cfgpkg.WebDAVGlobal  `json:"webdav"`
			Torrent *cfgpkg.TorrentGlobal `json:"torrent"`
			Fuse    *cfgpkg.FuseGlobal    `json:"fuse"`
			Log     *cfgpkg.Log           `json:"log"`
		}
		if err := ctx.ShouldBindJSON(&body); err != nil {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		// Basic validation
		if body.HTTP != nil {
			if body.HTTP.Port < 0 || body.HTTP.Port > 65535 {
				ctx.JSON(http.StatusBadRequest, gin.H{"error": "invalid HTTP port"})
				return
			}
			if body.HTTP.IP != "" && net.ParseIP(body.HTTP.IP) == nil {
				ctx.JSON(http.StatusBadRequest, gin.H{"error": "invalid HTTP IP"})
				return
			}
		}
		if body.WebDAV != nil {
			if body.WebDAV.Port < 0 || body.WebDAV.Port > 65535 {
				ctx.JSON(http.StatusBadRequest, gin.H{"error": "invalid WebDAV port"})
				return
			}
		}
		if body.Torrent != nil {
			if body.Torrent.AddTimeout < 0 || body.Torrent.ReadTimeout < 0 {
				ctx.JSON(http.StatusBadRequest, gin.H{"error": "timeouts must be >= 0"})
				return
			}
			if body.Torrent.IP != "" && net.ParseIP(body.Torrent.IP) == nil {
				ctx.JSON(http.StatusBadRequest, gin.H{"error": "invalid Public IP"})
				return
			}
			if body.Torrent.ListenPort < 0 || body.Torrent.ListenPort > 65535 {
				ctx.JSON(http.StatusBadRequest, gin.H{"error": "invalid listen port"})
				return
			}
			if body.Torrent.GlobalCacheSize < 0 {
				ctx.JSON(http.StatusBadRequest, gin.H{"error": "cache size must be >= 0"})
				return
			}
			if body.Torrent.ReaderPoolSize < 1 {
				ctx.JSON(http.StatusBadRequest, gin.H{"error": "reader pool size must be >= 1"})
				return
			}
			if body.Torrent.ReadaheadMB < 0 {
				ctx.JSON(http.StatusBadRequest, gin.H{"error": "readahead must be >= 0"})
				return
			}
		}
		if body.Fuse != nil {
			if strings.TrimSpace(body.Fuse.Path) == "" {
				ctx.JSON(http.StatusBadRequest, gin.H{"error": "FUSE path required"})
				return
			}
		}
		if err := s.SaveConfig(func(conf *cfgpkg.Root) {
			if body.HTTP != nil {
				conf.HTTPGlobal = body.HTTP
			}
			if body.WebDAV != nil {
				conf.WebDAV = body.WebDAV
			}
			if body.Torrent != nil {
				conf.Torrent = body.Torrent
				// also apply tuning live
				_ = s.ApplyTorrentTuning(body.Torrent.ReaderPoolSize, body.Torrent.ReadaheadMB)
			}
			if body.Fuse != nil {
				conf.Fuse = body.Fuse
			}
			if body.Log != nil {
				conf.Log = body.Log
			}
		}); err != nil {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		ctx.JSON(http.StatusOK, gin.H{"ok": true})
	}
}

// apiSetLimitsHandler sets rate limits (Mbit/s)
var apiSetLimitsHandler = func(s *torrent.Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		var body limitsPayload
		if err := ctx.ShouldBindJSON(&body); err != nil {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if err := s.SetLimits(body.DownloadMbit, body.UploadMbit); err != nil {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		// persist to config
		if err := s.SaveLimitsToConfig(body.DownloadMbit, body.UploadMbit); err != nil {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		ctx.JSON(http.StatusOK, gin.H{"ok": true})
	}
}

// qBittorrent API toggle endpoints
var apiGetQbtHandler = func() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		enabled := atomic.LoadInt32(&qbtEnabled) == 1
		ctx.JSON(http.StatusOK, gin.H{"enabled": enabled})
	}
}

var apiSetQbtHandler = func(s *torrent.Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		var req struct {
			Enabled bool `json:"enabled"`
		}
		if err := ctx.ShouldBindJSON(&req); err != nil {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		SetQbtEnabled(req.Enabled)
		_ = s.SaveQbtToConfig(req.Enabled)
		ctx.JSON(http.StatusOK, gin.H{"enabled": req.Enabled})
	}
}
