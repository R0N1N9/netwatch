package server

import (
	"net/http"
	"os"
	"path/filepath"
)

// handleAbout tells the console's UPDATE card where this copy lives, so the
// commands it shows can be pasted as they are.
func (s *Server) handleAbout(w http.ResponseWriter, r *http.Request) {
	exe, _ := os.Executable()
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	writeJSON(w, map[string]any{"build": buildStamp(), "exe": exe, "repo": repoOf(exe), "dataDir": s.DataDir, "demo": s.demo})
}

// repoOf returns the checkout a binary was built in — the folder above bin/
// that holds update.sh — or "" if it was installed somewhere else.
func repoOf(exe string) string {
	dir := filepath.Dir(filepath.Dir(exe))
	if filepath.Base(filepath.Dir(exe)) != "bin" {
		return ""
	}
	if _, err := os.Stat(filepath.Join(dir, "update.sh")); err != nil {
		return ""
	}
	return dir
}
