// Command stack runs only test-owned state and the real Stash binary behind a
// subpath reverse proxy. It never accepts an existing config/database path.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/stashapp/stash/internal/manager/config"
)

func run() error {
	binary, err := filepath.Abs("dist/stash-browser")
	if err != nil {
		return err
	}
	if _, err := os.Stat(binary); err != nil {
		return fmt.Errorf("build the browser binary first: %w", err)
	}
	root, err := os.MkdirTemp("", "stash-browser-stack-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(root)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	cfg := config.NewEmpty()
	configPath := filepath.Join(root, "config.yml")
	cfg.SetConfigFile(configPath)
	cfg.SetString(config.Host, "127.0.0.1")
	cfg.SetInt(config.Port, port)
	cfg.SetString(config.Username, "owner")
	cfg.SetPassword("stack +& password")
	cfg.SetString(config.ApiKey, "stack-test-api-key")
	cfg.SetBool(config.NoBrowser, true)
	cfg.SetBool(config.LogAccess, false)
	cfg.SetBool(config.SourceTrackingEnabled, false)
	for key, path := range map[string]string{config.Database: "library.sqlite", config.Generated: "generated", config.Cache: "cache", config.Metadata: "metadata", config.BlobsPath: "blobs", config.PluginsPath: "plugins", config.ScrapersPath: "scrapers"} {
		cfg.SetString(key, filepath.Join(root, path))
		if key != config.Database {
			if err := os.MkdirAll(filepath.Join(root, path), 0700); err != nil {
				return err
			}
		}
	}
	if err := cfg.SetInitialConfig(); err != nil {
		return err
	}
	if err := cfg.Write(); err != nil {
		return err
	}
	target, _ := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", port))
	var child *exec.Cmd
	stop := func() {
		if child != nil {
			_ = child.Process.Signal(syscall.SIGTERM)
			done := make(chan struct{})
			go func() { _ = child.Wait(); close(done) }()
			select {
			case <-done:
			case <-time.After(35 * time.Second):
				_ = child.Process.Kill()
				<-done
			}
			child = nil
		}
	}
	defer stop()
	start := func() error {
		child = exec.Command(binary)
		// Remove inherited Stash settings; only the temporary config is used.
		for _, entry := range os.Environ() {
			if !strings.HasPrefix(entry, "STASH_") {
				child.Env = append(child.Env, entry)
			}
		}
		child.Env = append(child.Env, "STASH_CONFIG_FILE="+configPath)
		child.Stdout, child.Stderr = os.Stdout, os.Stderr
		if err := child.Start(); err != nil {
			child = nil
			return err
		}
		client := &http.Client{Timeout: time.Second}
		deadline := time.Now().Add(45 * time.Second)
		for time.Now().Before(deadline) {
			res, err := client.Get(target.String() + "/healthz")
			if err == nil {
				_ = res.Body.Close()
				if res.StatusCode == 200 {
					return nil
				}
			}
			time.Sleep(100 * time.Millisecond)
		}
		return fmt.Errorf("test backend did not become healthy")
	}
	if err := start(); err != nil {
		return err
	}
	proxy := &httputil.ReverseProxy{Rewrite: func(r *httputil.ProxyRequest) {
		r.SetURL(target)
		r.Out.Host = r.In.Host
		r.Out.URL.Path = strings.TrimPrefix(r.Out.URL.Path, "/mounted")
		r.SetXForwarded()
		r.Out.Header.Set("X-Forwarded-Prefix", "/mounted")
	}}

	var controls sync.Mutex
	mux := http.NewServeMux()
	mux.Handle("/mounted/", proxy)
	mux.HandleFunc("POST /__test/{action}", func(w http.ResponseWriter, r *http.Request) {
		controls.Lock()
		defer controls.Unlock()
		var err error
		switch r.PathValue("action") {
		case "restart":
			stop()
			err = start()
		case "block-config":
			err = os.Rename(configPath, configPath+".saved")
			if err == nil {
				err = os.Mkdir(configPath, 0700)
			}
		case "restore-config":
			err = os.Remove(configPath)
			if err == nil {
				err = os.Rename(configPath+".saved", configPath)
			}
		default:
			http.NotFound(w, r)
			return
		}
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	server := &http.Server{Addr: "127.0.0.1:3036", Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	go func() { <-ctx.Done(); _ = server.Close() }()
	log.Printf("Isolated browser stack ready at http://127.0.0.1:3036/mounted/")
	err = server.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}
