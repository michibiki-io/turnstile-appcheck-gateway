package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
)

const maxJSONBodyBytes = 1 << 20

func decodeJSONStrict(r io.Reader, out any) error {
	dec := json.NewDecoder(io.LimitReader(r, maxJSONBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return err
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return fmt.Errorf("invalid json: extra data")
		}
		return err
	}
	return nil
}

func hasJSONContentType(r *http.Request) bool {
	ct := strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Type")))
	if ct == "" {
		return false
	}
	return strings.HasPrefix(ct, "application/json")
}

func extractRemoteIP(r *http.Request, trustProxyHeaders bool) string {
	if trustProxyHeaders {
		for _, header := range []string{"CF-Connecting-IP", "X-Forwarded-For", "X-Real-IP"} {
			value := strings.TrimSpace(r.Header.Get(header))
			if value == "" {
				continue
			}
			if header == "X-Forwarded-For" {
				value = strings.TrimSpace(strings.Split(value, ",")[0])
			}
			if ip := net.ParseIP(value); ip != nil {
				return value
			}
		}
	}

	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err == nil {
		if ip := net.ParseIP(host); ip != nil {
			return host
		}
	}

	candidate := strings.TrimSpace(r.RemoteAddr)
	if ip := net.ParseIP(candidate); ip != nil {
		return candidate
	}

	return ""
}
