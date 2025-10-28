package http

import (
	"net/http"
	"strings"
	"sync/atomic"

	"github.com/gin-gonic/gin"
	"github.com/jkaberg/distribyted/torrent"
)

// registerQBittorrentAPI wires the minimal set of qBittorrent-compatible endpoints used by Arr apps.
func registerQBittorrentAPI(rg *gin.RouterGroup, ss *torrent.Stats, s *torrent.Service) {
	// Auth
	rg.POST("/auth/login", qbtAuthLogin())
	rg.POST("/auth/logout", qbtAuthLogout())

	// App meta
	rg.GET("/app/version", func(c *gin.Context) { c.String(http.StatusOK, "v4.0.0") })
	rg.GET("/app/webapiVersion", func(c *gin.Context) { c.String(http.StatusOK, "2.8.0") })
	rg.GET("/app/preferences", qbtGuard(qbtAppPreferences(s)))

	// Torrents
	rg.POST("/torrents/add", qbtGuard(qbtTorrentsAdd(s)))
	rg.GET("/torrents/info", qbtGuard(qbtTorrentsInfo(ss, s)))
	rg.POST("/torrents/delete", qbtGuard(qbtTorrentsDelete(ss, s)))

	// Categories (mapped to routes)
	rg.GET("/torrents/categories", qbtGuard(qbtCategoriesList(ss, s)))
	rg.POST("/torrents/createCategory", qbtGuard(qbtCategoryCreate(s)))
	rg.POST("/torrents/setCategory", qbtGuard(qbtCategorySet(s)))
}

func qbtAuthLogin() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Accept any credentials, set a dummy SID cookie
		http.SetCookie(c.Writer, &http.Cookie{Name: "SID", Value: "ok", Path: "/", HttpOnly: true})
		c.String(http.StatusOK, "Ok.")
	}
}

func qbtAuthLogout() gin.HandlerFunc {
	return func(c *gin.Context) {
		http.SetCookie(c.Writer, &http.Cookie{Name: "SID", Value: "", Path: "/", MaxAge: -1, HttpOnly: true})
		c.String(http.StatusOK, "Ok.")
	}
}

// Helpers
func splitCSV(v string) []string {
	if v == "" {
		return nil
	}
	parts := strings.Split(v, "|")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// runtime toggle
var qbtEnabled int32

func SetQbtEnabled(v bool) {
	if v {
		atomic.StoreInt32(&qbtEnabled, 1)
	} else {
		atomic.StoreInt32(&qbtEnabled, 0)
	}
}

func qbtGuard(h gin.HandlerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		if atomic.LoadInt32(&qbtEnabled) == 0 {
			c.String(http.StatusNotFound, "")
			return
		}
		h(c)
	}
}

// qbtAppPreferences returns minimal preferences used by Arr apps
func qbtAppPreferences(s *torrent.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"save_path":                s.GetFuseBasePath(),
			"temp_path_enabled":        false,
			"temp_path":                "",
			"create_subfolder_enabled": false,
			"auto_tmm_enabled":         false,
		})
	}
}
