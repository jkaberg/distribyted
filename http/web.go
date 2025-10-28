package http

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/jkaberg/distribyted/torrent"
)

//var indexHandler = func(c *gin.Context) {
//	// Redirect to routes page as the default landing page
//	c.Redirect(http.StatusFound, "/routes")
//}

var indexHandler = func(ss *torrent.Stats) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Avoid heavy pre-render stats; UI fetches data asynchronously
		c.HTML(http.StatusOK, "routes.html", nil)
	}
}

var logsHandler = func(c *gin.Context) {
	c.HTML(http.StatusOK, "logs.html", nil)
}

var settingsHandler = func(c *gin.Context) {
	c.HTML(http.StatusOK, "settings.html", nil)
}
