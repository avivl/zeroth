// Command spike is the throwaway BA-6 confirmation-spike process.
//
// Listen on :8421, report health and whether ANTHROPIC_API_KEY is set,
// and list fixture tar sizes. No UI, no auth product, no Linear.
package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"time"

	spike "github.com/avivl/zeroth/zeroth-spike"
)

func main() {
	addr := flag.String("addr", envOr("SPIKE_ADDR", ":8421"), "listen address")
	fixtures := flag.String("fixtures", envOr("SPIKE_FIXTURES", "./fixtures"), "fixture tar directory")
	check := flag.Bool("check", false, "GET /health on -addr and exit")
	flag.Parse()

	if *check {
		if err := checkHealth(*addr); err != nil {
			fmt.Fprintf(os.Stderr, "spike check: %v\n", err)
			os.Exit(1)
		}
		os.Exit(0)
	}

	mux := spike.NewMux(spike.ServerConfig{FixturesDir: *fixtures})
	srv := &http.Server{
		Addr:              *addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	fmt.Fprintf(os.Stderr, "spike listening on %s (fixtures %s)\n", *addr, *fixtures)
	if err := srv.ListenAndServe(); err != nil {
		fmt.Fprintf(os.Stderr, "spike listen: %v\n", err)
		os.Exit(1)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func checkHealth(addr string) error {
	url := "http://" + addr + "/health"
	client := &http.Client{Timeout: 2 * time.Second}
	res, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("get %s: %w", url, err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: status %d", url, res.StatusCode)
	}
	return nil
}
