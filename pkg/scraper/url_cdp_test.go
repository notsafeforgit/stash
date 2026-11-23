package scraper

import (
	"bufio"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// These tests drive a real browser. Set STASH_TEST_CHROME to a Chrome or
// Chromium executable to run them.
func testChromePath(t *testing.T) string {
	t.Helper()
	path := os.Getenv("STASH_TEST_CHROME")
	if path == "" {
		t.Skip("set STASH_TEST_CHROME to a Chrome or Chromium executable")
	}
	return path
}

type cdpTestConfig struct {
	mockGlobalConfig
	cdpPath string
	proxy   string
}

func (c cdpTestConfig) GetScraperCDPPath() string { return c.cdpPath }
func (c cdpTestConfig) GetProxy() string          { return c.proxy }

func readCDPResult(t *testing.T, r io.Reader, err error) string {
	t.Helper()
	if err != nil {
		t.Fatalf("loading the page through Chrome: %v", err)
	}
	body, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func renderedPageServer(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		cookie := ""
		if c, err := r.Cookie("session"); err == nil {
			cookie = c.Value
		}
		fmt.Fprintf(w, `<html><body>
<p id="header">header:%s</p>
<p id="cookie">cookie:%s</p>
<p id="script"></p>
<button id="more" onclick="document.getElementById('clicked').textContent = 'clicked:yes'">More</button>
<p id="clicked"></p>
<script>document.getElementById('script').textContent = 'script:ran'</script>
</body></html>`, r.Header.Get("X-Stash-Test"), cookie)
	}))
	t.Cleanup(server.Close)
	return server
}

func renderedPageOptions() scraperDriverOptions {
	return scraperDriverOptions{
		UseCDP: true,
		Sleep:  1,
		Clicks: []*clickOptions{{XPath: `//button[@id="more"]`, Sleep: 1}},
		Cookies: []*cookieOptions{{
			CookieURL: "http://127.0.0.1",
			Cookies:   []*scraperCookies{{Name: "session", Value: "abc", Domain: "127.0.0.1", Path: "/"}},
		}},
		Headers: []*header{{Key: "X-Stash-Test", Value: "sent"}},
	}
}

func expectRenderedPage(t *testing.T, page string) {
	t.Helper()
	for _, want := range []string{"header:sent", "cookie:abc", "script:ran", "clicked:yes"} {
		if !strings.Contains(page, want) {
			t.Errorf("page is missing %q:\n%s", want, page)
		}
	}
}

func TestCDPLoadsRenderedPage(t *testing.T) {
	chrome := testChromePath(t)
	server := renderedPageServer(t)

	r, err := urlFromCDP(t.Context(), server.URL+"/", renderedPageOptions(), cdpTestConfig{cdpPath: chrome})
	expectRenderedPage(t, readCDPResult(t, r, err))
}

// startRemoteChrome runs a browser the way a separate Chrome container
// would, listening for DevTools connections on a local port.
func startRemoteChrome(t *testing.T, chrome string) string {
	t.Helper()
	dir := t.TempDir()
	cmd := exec.Command(chrome,
		"--headless",
		"--no-first-run",
		"--no-default-browser-check",
		"--remote-debugging-address=127.0.0.1",
		"--remote-debugging-port=0",
		"--user-data-dir="+dir,
		"about:blank",
	)
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting Chrome: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})

	portFile := filepath.Join(dir, "DevToolsActivePort")
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if f, err := os.Open(portFile); err == nil {
			scanner := bufio.NewScanner(f)
			port := ""
			if scanner.Scan() {
				port = strings.TrimSpace(scanner.Text())
			}
			f.Close()
			if port != "" {
				return "127.0.0.1:" + port
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("Chrome did not report its DevTools port")
	return ""
}

func TestCDPUsesRemoteBrowser(t *testing.T) {
	chrome := testChromePath(t)
	server := renderedPageServer(t)
	address := startRemoteChrome(t, chrome)

	for _, cdpPath := range []string{
		"http://" + address + "/json/version",
		"ws://" + address + "/",
	} {
		t.Run(cdpPath, func(t *testing.T) {
			r, err := urlFromCDP(t.Context(), server.URL+"/", renderedPageOptions(), cdpTestConfig{cdpPath: cdpPath})
			expectRenderedPage(t, readCDPResult(t, r, err))
		})
	}
}

// authenticatingProxy serves every page itself, so Chrome never resolves the
// page's host, and counts the credentials it is offered.
func authenticatingProxy(t *testing.T, user, pass string) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	want := "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+pass))
	var attempts atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := r.Header.Get("Proxy-Authorization")
		if got != "" {
			attempts.Add(1)
		}
		if got != want {
			w.Header().Set("Proxy-Authenticate", `Basic realm="stash-test"`)
			w.WriteHeader(http.StatusProxyAuthRequired)
			return
		}
		fmt.Fprintf(w, "<html><body><p>proxied:%s%s</p></body></html>", r.URL.Host, r.URL.Path)
	}))
	t.Cleanup(proxy.Close)
	return proxy, &attempts
}

func TestCDPAuthenticatesWithProxy(t *testing.T) {
	chrome := testChromePath(t)
	proxy, _ := authenticatingProxy(t, "scraper", "secret")
	proxyURL := strings.Replace(proxy.URL, "http://", "http://scraper:secret@", 1)

	r, err := urlFromCDP(t.Context(), "http://scrape.test/page", scraperDriverOptions{UseCDP: true, Sleep: 1}, cdpTestConfig{cdpPath: chrome, proxy: proxyURL})
	page := readCDPResult(t, r, err)
	if !strings.Contains(page, "proxied:scrape.test/page") {
		t.Errorf("page did not come through the proxy:\n%s", page)
	}
}

func TestCDPStopsAfterRejectedProxyCredentials(t *testing.T) {
	chrome := testChromePath(t)
	proxy, attempts := authenticatingProxy(t, "scraper", "secret")
	proxyURL := strings.Replace(proxy.URL, "http://", "http://scraper:wrong@", 1)

	start := time.Now()
	r, err := urlFromCDP(t.Context(), "http://scrape.test/page", scraperDriverOptions{UseCDP: true, Sleep: 1}, cdpTestConfig{cdpPath: chrome, proxy: proxyURL})
	if err == nil {
		body, _ := io.ReadAll(r)
		if strings.Contains(string(body), "proxied:") {
			t.Fatalf("the proxy accepted the wrong password:\n%s", body)
		}
	}
	if elapsed := time.Since(start); elapsed >= scrapeGetTimeout/2 {
		t.Errorf("rejected credentials took %s; they should fail without waiting for the scrape timeout", elapsed)
	}
	if n := attempts.Load(); n > 2 {
		t.Errorf("Chrome offered the rejected credentials %d times", n)
	}
}
