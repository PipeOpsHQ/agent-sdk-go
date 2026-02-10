package api

import (
	"fmt"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func (s *Server) handleFileView(w http.ResponseWriter, r *http.Request, _ principal) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
		return
	}
	path, err := sanitizeRequestedFilePath(strings.TrimSpace(r.URL.Query().Get("path")))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	content, stat, err := readFileForResponse(path)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	ctype := detectContentType(path, content)
	w.Header().Set("Content-Type", ctype)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(content)))
	w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=%q", stat.Name()))
	_, _ = w.Write(content)
}

func (s *Server) handleFileDownload(w http.ResponseWriter, r *http.Request, _ principal) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
		return
	}
	path, err := sanitizeRequestedFilePath(strings.TrimSpace(r.URL.Query().Get("path")))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	content, stat, err := readFileForResponse(path)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	ctype := detectContentType(path, content)
	w.Header().Set("Content-Type", ctype)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(content)))
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", stat.Name()))
	_, _ = w.Write(content)
}

func sanitizeRequestedFilePath(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("path is required")
	}
	cleaned := filepath.Clean(path)
	if !filepath.IsAbs(cleaned) {
		abs, err := filepath.Abs(cleaned)
		if err != nil {
			return "", err
		}
		cleaned = abs
	}
	return cleaned, nil
}

func readFileForResponse(path string) ([]byte, os.FileInfo, error) {
	stat, err := os.Stat(path)
	if err != nil {
		return nil, nil, err
	}
	if stat.IsDir() {
		return nil, nil, fmt.Errorf("path is a directory")
	}
	if stat.Size() > 50*1024*1024 {
		return nil, nil, fmt.Errorf("file too large (max 50MB)")
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	return content, stat, nil
}

func detectContentType(path string, content []byte) string {
	ext := strings.ToLower(filepath.Ext(path))
	if ext != "" {
		if c := mime.TypeByExtension(ext); c != "" {
			return c
		}
	}
	if len(content) == 0 {
		return "application/octet-stream"
	}
	return http.DetectContentType(content)
}
