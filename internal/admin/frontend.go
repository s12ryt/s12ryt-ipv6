package admin

import (
	"bytes"
	"errors"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"
	"time"
)

func (s *HTTPServer) SetFrontend(frontend fs.FS) error {
	if frontend == nil {
		return errors.New("frontend filesystem is required")
	}
	index, err := fs.ReadFile(frontend, "index.html")
	if err != nil {
		return errors.New("frontend index is unavailable")
	}
	info, err := fs.Stat(frontend, "index.html")
	if err != nil || !info.Mode().IsRegular() {
		return errors.New("frontend index must be a regular file")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.frontendSet {
		return errors.New("frontend service is already configured")
	}
	s.mux.Handle("GET /", frontendHandler(frontend, index))
	s.frontendSet = true
	return nil
}

func frontendHandler(frontend fs.FS, index []byte) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		name := strings.TrimPrefix(path.Clean(request.URL.Path), "/")
		if name == "." || name == "" {
			serveFrontendContent(response, request, "index.html", index, "no-store")
			return
		}
		if !fs.ValidPath(name) {
			http.NotFound(response, request)
			return
		}

		content, err := fs.ReadFile(frontend, name)
		if err == nil {
			info, statErr := fs.Stat(frontend, name)
			if statErr != nil || !info.Mode().IsRegular() {
				http.NotFound(response, request)
				return
			}
			serveFrontendContent(response, request, name, content, "public, max-age=31536000, immutable")
			return
		}

		if strings.HasPrefix(name, "assets/") || path.Ext(name) != "" {
			http.NotFound(response, request)
			return
		}
		serveFrontendContent(response, request, "index.html", index, "no-store")
	})
}

func serveFrontendContent(response http.ResponseWriter, request *http.Request, name string, content []byte, cacheControl string) {
	response.Header().Set("Cache-Control", cacheControl)
	response.Header().Set("X-Content-Type-Options", "nosniff")
	if contentType := mime.TypeByExtension(path.Ext(name)); contentType != "" {
		response.Header().Set("Content-Type", contentType)
	}
	http.ServeContent(response, request, name, time.Time{}, bytes.NewReader(content))
}
