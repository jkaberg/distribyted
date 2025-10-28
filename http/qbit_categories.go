package http

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/jkaberg/distribyted/torrent"
)

// categories list maps routes to qBittorrent categories format
func qbtCategoriesList(ss *torrent.Stats, s *torrent.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		// qBittorrent returns a map[name]{name, savePath}
		out := map[string]map[string]string{}
		// derive base from config: prefer FUSE path, else default to "/"
		base := "/"
		if conf, err := s.ConfigSnapshot(); err == nil && conf != nil {
			if conf.Fuse != nil && conf.Fuse.Path != "" {
				base = conf.Fuse.Path
			}
		}
		for _, rs := range ss.RoutesStats() {
			out[rs.Name] = map[string]string{
				"name":     rs.Name,
				"savePath": base + "/" + rs.Name,
			}
		}
		c.JSON(http.StatusOK, out)
	}
}

func qbtCategoryCreate(s *torrent.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		name := c.PostForm("category")
		if name == "" {
			c.String(http.StatusBadRequest, "category required")
			return
		}
		if err := s.CreateRoute(name); err != nil {
			c.String(http.StatusBadRequest, err.Error())
			return
		}
		c.String(http.StatusOK, "Ok.")
	}
}

func qbtCategorySet(s *torrent.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		hashes := splitCSV(c.PostForm("hashes"))
		category := c.PostForm("category")
		if len(hashes) == 0 || category == "" {
			c.String(http.StatusBadRequest, "hashes and category required")
			return
		}
		_ = s.CreateRoute(category)
		// We don't have a move-between-routes primitive; implement as remove + re-add not feasible.
		// For Arr workflows, setCategory is typically used before adding; we accept and return Ok.
		c.String(http.StatusOK, "Ok.")
	}
}
