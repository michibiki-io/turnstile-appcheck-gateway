package adminui

import (
	"embed"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"

	"github.com/gin-gonic/gin"
)

//go:embed dist/* dist/assets/*
var assets embed.FS

func Register(root *gin.RouterGroup, basePath string, middleware ...gin.HandlerFunc) {
	basePath = "/" + strings.Trim(strings.TrimSpace(basePath), "/")
	ui := root.Group(basePath, middleware...)
	ui.GET("", redirectToSlash())
	ui.GET("/*filepath", func(c *gin.Context) {
		name := strings.TrimPrefix(c.Param("filepath"), "/")
		if name == "" {
			name = "index.html"
		}
		if name == "config.js" {
			configJS(root.BasePath(), basePath)(c)
			return
		}
		if fileExists(name) {
			serveAsset(name)(c)
			return
		}
		serveAsset("index.html")(c)
	})
}

func configJS(contextPath, dashboardBasePath string) gin.HandlerFunc {
	return func(c *gin.Context) {
		prefix := strings.TrimRight(contextPath, "/")
		c.Header("Content-Type", "application/javascript; charset=utf-8")
		c.String(http.StatusOK, `window.TACG_ADMIN = {
  apiBasePath: %q,
  dashboardBasePath: %q
};`, prefix+"/_admin/api/v1", prefix+dashboardBasePath)
	}
}

func redirectToSlash() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Redirect(http.StatusMovedPermanently, strings.TrimRight(c.Request.URL.Path, "/")+"/")
	}
}

func serveAsset(name string) gin.HandlerFunc {
	return func(c *gin.Context) {
		data, err := assets.ReadFile("dist/" + path.Clean(name))
		if err != nil {
			c.Status(http.StatusNotFound)
			return
		}
		contentType := mime.TypeByExtension(path.Ext(name))
		if contentType != "" {
			c.Header("Content-Type", contentType)
		}
		c.Data(http.StatusOK, contentType, data)
	}
}

func fileExists(name string) bool {
	info, err := fs.Stat(assets, "dist/"+path.Clean(name))
	return err == nil && !info.IsDir()
}
