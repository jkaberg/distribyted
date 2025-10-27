package http

import (
	"fmt"
	"net/http"
	"path/filepath"
	"time"

	"github.com/distribyted/distribyted/torrent"
	"github.com/gin-gonic/gin"
)

// qBittorrent torrent info DTO (subset Arr uses)
type qbtTorrentInfo struct {
	Hash     string  `json:"hash"`
	Name     string  `json:"name"`
	Category string  `json:"category"`
	State    string  `json:"state"`
	Progress float64 `json:"progress"`
	Size     int64   `json:"size"`
	DlSpeed  int64   `json:"dlspeed"`
	UpSpeed  int64   `json:"upspeed"`
	AddedOn  int64   `json:"added_on"`
	SavePath string  `json:"save_path"`
}

func qbtTorrentsAdd(s *torrent.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		category := c.PostForm("category")
		if category == "" {
			category = "default"
		}
		// Ensure/create route for category
		_ = s.CreateRoute(category)

		urls := c.PostForm("urls")
		if urls != "" {
			for _, u := range splitCSV(urls) {
				if u == "" {
					continue
				}
				if err := s.AddMagnet(category, u); err != nil {
					c.String(http.StatusBadRequest, err.Error())
					return
				}
			}
			c.String(http.StatusOK, "Ok.")
			return
		}

		// Fallback to file upload key "torrents" (single .torrent supported)
		fh, err := c.FormFile("torrents")
		if err != nil {
			c.String(http.StatusBadRequest, "No urls or torrents provided")
			return
		}
		if filepath.Ext(fh.Filename) != ".torrent" {
			c.String(http.StatusBadRequest, "only .torrent files allowed")
			return
		}
		folder, err := s.EnsureRouteFolder(category)
		if err != nil {
			c.String(http.StatusBadRequest, err.Error())
			return
		}
		dst := filepath.Join(folder, filepath.Base(fh.Filename))
		if err := c.SaveUploadedFile(fh, dst); err != nil {
			c.String(http.StatusBadRequest, err.Error())
			return
		}
		if _, err := s.AddTorrentPath(category, dst); err != nil {
			c.String(http.StatusBadRequest, err.Error())
			return
		}
		c.String(http.StatusOK, "Ok.")
	}
}

func qbtTorrentsInfo(ss *torrent.Stats, s *torrent.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		category := c.Query("category")
		hashesQ := c.Query("hashes")
		filterByHashes := map[string]struct{}{}
		if hashesQ != "" {
			for _, h := range splitCSV(hashesQ) {
				filterByHashes[h] = struct{}{}
			}
		}

		var out []qbtTorrentInfo
		if category != "" {
			// Single route page; size large to include all
			rp := ss.RouteStatsPage(category, 1, 10_000)
			for _, it := range rp.Items {
				if hashesQ != "" {
					if _, ok := filterByHashes[it.Hash]; !ok {
						continue
					}
				}
				out = append(out, mapTorrentInfoWithBase(s, category, it))
			}
		} else {
			for _, rs := range ss.RoutesStats() {
				for _, it := range rs.TorrentStats {
					if hashesQ != "" {
						if _, ok := filterByHashes[it.Hash]; !ok {
							continue
						}
					}
					out = append(out, mapTorrentInfoWithBase(s, rs.Name, it))
				}
			}
		}
		c.JSON(http.StatusOK, out)
	}
}

func qbtTorrentsDelete(ss *torrent.Stats, s *torrent.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		hashesQ := c.PostForm("hashes")
		category := c.PostForm("category") // optional
		if hashesQ == "" {
			c.String(http.StatusBadRequest, "hashes required")
			return
		}
		for _, h := range splitCSV(hashesQ) {
			if h == "" {
				continue
			}
			// Resolve route if not provided
			route := category
			if route == "" {
				if r := ss.RouteOf(h); r != "" {
					route = r
				} else {
					// fallback to try DB-less removal across known routes
					// attempt local removal by guessing common categories
					// but if unknown, return error
					c.String(http.StatusBadRequest, "category or known route required for deletion")
					return
				}
			}
			if err := s.RemoveFromHash(route, h); err != nil {
				if err := s.RemoveFromHashLocal(route, h); err != nil {
					c.String(http.StatusBadRequest, fmt.Sprintf("%s", err))
					return
				}
			}
		}
		c.String(http.StatusOK, "Ok.")
	}
}

func mapTorrentInfo(route string, ts *torrent.TorrentStats) qbtTorrentInfo {
	// Report complete immediately per requirement
	return qbtTorrentInfo{
		Hash:     ts.Hash,
		Name:     ts.Name,
		Category: route,
		State:    "completed",
		Progress: 1.0,
		Size:     0,
		DlSpeed:  ts.DownloadedBytes,
		UpSpeed:  ts.UploadedBytes,
		AddedOn:  time.Now().Unix(),
		SavePath: filepath.Join("/", route),
	}
}

func mapTorrentInfoWithBase(s *torrent.Service, route string, ts *torrent.TorrentStats) qbtTorrentInfo {
	base := "/"
	if conf, err := s.ConfigSnapshot(); err == nil && conf != nil {
		if conf.Fuse != nil && conf.Fuse.Path != "" {
			base = conf.Fuse.Path
		}
	}
	ti := mapTorrentInfo(route, ts)
	ti.SavePath = filepath.Join(base, route)
	return ti
}
