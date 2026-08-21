package spike

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"

	"github.com/avivl/zeroth/zeroth-spike/harness"
)

// ServerConfig is the spike HTTP surface.
type ServerConfig struct {
	FixturesDir string
}

type healthResponse struct {
	Status string `json:"status"`
}

type authResponse struct {
	APIKeyConfigured bool   `json:"api_key_configured"`
	Method           string `json:"method"`
}

type fixtureRow struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Size int64  `json:"size_bytes"`
}

type fixturesResponse struct {
	Dir   string       `json:"dir"`
	Items []fixtureRow `json:"items"`
}

// NewMux returns the spike HTTP handlers.
func NewMux(cfg ServerConfig) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, healthResponse{Status: "ok"})
	})
	mux.HandleFunc("GET /auth", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, authResponse{
			APIKeyConfigured: harness.APIKeyConfiguredBool(),
			Method:           "api_key",
		})
	})
	mux.HandleFunc("GET /fixtures", func(w http.ResponseWriter, _ *http.Request) {
		dir := cfg.FixturesDir
		names := []string{"S.tar", "M.tar", "L.tar"}
		items := make([]fixtureRow, 0, len(names))
		for _, name := range names {
			row := fixtureRow{Name: name, Path: filepath.Join(dir, name)}
			if st, err := os.Stat(row.Path); err == nil && !st.IsDir() {
				row.Size = st.Size()
			}
			items = append(items, row)
		}
		writeJSON(w, http.StatusOK, fixturesResponse{Dir: dir, Items: items})
	})
	return mux
}

func writeJSON[T healthResponse | authResponse | fixturesResponse](w http.ResponseWriter, status int, v T) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
