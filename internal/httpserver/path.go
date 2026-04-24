package httpserver

import "strings"

// JoinSubpath joins base subpath and API suffix without duplicate slashes.
func JoinSubpath(subpath, suffix string) string {
	cleanSubpath := strings.TrimSpace(subpath)
	if cleanSubpath == "" {
		cleanSubpath = "/"
	}
	if !strings.HasPrefix(cleanSubpath, "/") {
		cleanSubpath = "/" + cleanSubpath
	}
	cleanSubpath = strings.TrimSuffix(cleanSubpath, "/")
	if cleanSubpath == "" {
		cleanSubpath = "/"
	}

	cleanSuffix := strings.TrimSpace(suffix)
	if cleanSuffix == "" {
		cleanSuffix = "/"
	}
	if !strings.HasPrefix(cleanSuffix, "/") {
		cleanSuffix = "/" + cleanSuffix
	}

	if cleanSubpath == "/" {
		return cleanSuffix
	}
	return cleanSubpath + cleanSuffix
}
