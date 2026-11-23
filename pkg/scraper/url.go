package scraper

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/remote"
	"github.com/enetx/g"
	"github.com/enetx/surf"
	jsoniter "github.com/json-iterator/go"
	"golang.org/x/net/html/charset"

	"github.com/stashapp/stash/pkg/logger"
)

const scrapeDefaultSleep = time.Second * 2

var proxyAuthRE = regexp.MustCompile(`^(https?:\/\/)(([\P{Cc}]+):([\P{Cc}]+)@)?(([a-zA-Z0-9][a-zA-Z0-9.-]*)(:[0-9]{1,5})?)`)

func loadURL(ctx context.Context, loadURL string, client *http.Client, def Definition, globalConfig GlobalConfig) (io.Reader, error) {
	driverOptions := def.DriverOptions
	if driverOptions != nil {
		switch {
		case driverOptions.UseCDP:
			return urlFromCDP(ctx, loadURL, *driverOptions, globalConfig)
		case driverOptions.UseSurf:
			return urlFromSurf(ctx, loadURL, *driverOptions, def, globalConfig)
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, loadURL, nil)
	if err != nil {
		return nil, err
	}

	jar, err := def.jar()
	if err != nil {
		return nil, fmt.Errorf("error creating cookie jar: %w", err)
	}

	u, err := url.Parse(loadURL)
	if err != nil {
		return nil, fmt.Errorf("error parsing url %s: %w", loadURL, err)
	}

	// Fetch relevant cookies from the jar for url u and add them to the request
	cookies := jar.Cookies(u)
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}

	userAgent := globalConfig.GetScraperUserAgent()
	if userAgent != "" {
		req.Header.Set("User-Agent", userAgent)
	}

	if driverOptions != nil { // setting the Headers after the UA allows us to override it from inside the scraper
		for _, h := range driverOptions.Headers {
			if h.Key != "" {
				req.Header.Set(h.Key, h.Value)
				logger.Debugf("[scraper] adding header <%s:%s>", h.Key, h.Value)
			}
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("http error %d:%s", resp.StatusCode, http.StatusText(resp.StatusCode))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	bodyReader := bytes.NewReader(body)
	printCookies(jar, def, "Jar cookies found for scraper urls")
	return charset.NewReader(bodyReader, resp.Header.Get("Content-Type"))
}

// func urlFromSurf uses enetx/surf with TLS browser emulation to bypass fingerprint-based blocking.
// this is a step down from CDP but faster and more lightweight and can succeed where CDP might fail
func urlFromSurf(ctx context.Context, loadURL string, driverOptions scraperDriverOptions, def Definition, globalConfig GlobalConfig) (io.Reader, error) {
	// get cookies
	jar, err := def.jar()
	if err != nil {
		return nil, fmt.Errorf("error creating cookie jar: %w", err)
	}

	// create client
	builder := surf.NewClient().
		Builder().
		Impersonate().Chrome()

	// get global proxy
	proxyURL := globalConfig.GetProxy()
	if proxyURL != "" {
		builder = builder.Proxy(g.String(proxyURL))
	}

	client := builder.
		Build().
		Unwrap().
		Std() // use stdlib adapter

	// pass in jar directly
	client.Jar = jar

	// try to mimic normal method above
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, loadURL, nil)
	if err != nil {
		return nil, err
	}
	// remove User-Agent header. This undermines TLS fingerprinting
	// because of GREASE (RFC 8701)
	// older fingerprints + UAs are still sustainable.
	for _, h := range driverOptions.Headers {
		if h.Key != "" {
			if strings.ToLower(h.Key) == "user-agent" {
				continue
			}
			req.Header.Set(h.Key, h.Value)
			logger.Debugf("[scraper] adding header <%s:%s>", h.Key, h.Value)
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("http error %d:%s", resp.StatusCode, http.StatusText(resp.StatusCode))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	bodyReader := bytes.NewReader(body)
	printCookies(jar, def, "Jar cookies found for scraper urls")
	return charset.NewReader(bodyReader, resp.Header.Get("Content-Type"))
}

// func urlFromCDP uses chrome cdp and DOM to load and process the url
// if remote is set as true in the scraperConfig  it will try to use localhost:9222
// else it will look for google-chrome in path
func urlFromCDP(ctx context.Context, urlCDP string, driverOptions scraperDriverOptions, globalConfig GlobalConfig) (io.Reader, error) {
	if !driverOptions.UseCDP {
		return nil, fmt.Errorf("url shouldn't be fetched through CDP")
	}

	sleepDuration := scrapeDefaultSleep

	if driverOptions.Sleep > 0 {
		sleepDuration = time.Duration(driverOptions.Sleep) * time.Second
	}

	// if scraperCDPPath is a remote address, then allocate accordingly
	cdpPath := globalConfig.GetScraperCDPPath()
	if cdpPath != "" {
		var cancelAct context.CancelFunc

		if isCDPPathHTTP(globalConfig) || isCDPPathWS(globalConfig) {
			remoteURL := cdpPath

			// -------------------------------------------------------------------
			// #1023
			// when chromium is listening over RDP it only accepts requests
			// with host headers that are either IPs or `localhost`
			cdpURL, err := url.Parse(remoteURL)
			if err != nil {
				return nil, fmt.Errorf("failed to parse CDP Path: %v", err)
			}
			hostname := cdpURL.Hostname()
			if hostname != "localhost" {
				if net.ParseIP(hostname) == nil { // not an IP
					addr, err := net.LookupIP(hostname)
					if err != nil || len(addr) == 0 { // can not resolve to IP
						return nil, fmt.Errorf("CDP: hostname <%s> can not be resolved", hostname)
					}
					if len(addr[0]) == 0 { // nil IP
						return nil, fmt.Errorf("CDP: hostname <%s> resolved to nil", hostname)
					}
					// addr is a valid IP
					// replace the host part of the cdpURL with the IP
					cdpURL.Host = strings.Replace(cdpURL.Host, hostname, addr[0].String(), 1)
					// use that for remote
					remoteURL = cdpURL.String()
				}
			}
			// --------------------------------------------------------------------

			// if CDPPath is http(s) then we need to get the websocket URL
			if isCDPPathHTTP(globalConfig) {
				var err error
				remoteURL, err = getRemoteCDPWSAddress(ctx, remoteURL)
				if err != nil {
					return nil, err
				}
			}

			ctx, cancelAct = remote.NewAllocator(ctx, remoteURL)
		} else {
			// use a temporary user directory for chrome
			dir, err := os.MkdirTemp("", "stash-chromedp")
			if err != nil {
				return nil, err
			}
			defer os.RemoveAll(dir)

			opts := append(chromedp.DefaultExecAllocatorOptions[:],
				chromedp.UserDataDir(dir),
				chromedp.ExecPath(cdpPath),
			)
			if globalConfig.GetProxy() != "" {
				url, _, _ := splitProxyAuth(globalConfig.GetProxy())
				opts = append(opts, chromedp.ProxyServer(url))
			}

			ctx, cancelAct = chromedp.NewExecAllocator(ctx, opts...)
		}

		defer cancelAct()
	}

	ctx, cancel := chromedp.NewContext(ctx)
	defer cancel()

	// add a fixed timeout for the http request
	ctx, cancel = context.WithTimeout(ctx, scrapeGetTimeout)
	defer cancel()

	headers := cdpHeaders(driverOptions)

	if proxyUsesAuth(globalConfig.GetProxy()) {
		_, user, pass := splitProxyAuth(globalConfig.GetProxy())
		if err := answerCDPProxyAuth(ctx, user, pass); err != nil {
			return nil, err
		}
	}

	if err := chromedp.Do(ctx,
		cdpCommand(network.Enable, network.EnableParams{}),
		setCDPCookies(driverOptions),
		printCDPCookies(driverOptions, "Cookies found"),
		cdpCommand(network.SetExtraHTTPHeaders, network.SetExtraHTTPHeadersParams{Headers: network.Headers(headers)}),
		chromedp.Navigate(urlCDP),
		chromedp.Sleep(sleepDuration),
		setCDPClicks(driverOptions),
	); err != nil {
		return nil, err
	}

	res, err := chromedp.Run(ctx, chromedp.OuterHTML(chromedp.CSS("html")))
	if err != nil {
		return nil, err
	}

	if err := chromedp.Do(ctx, printCDPCookies(driverOptions, "Cookies set")); err != nil {
		return nil, err
	}

	return strings.NewReader(res), nil
}

func cdpCommand[P, R any](command cdp.Command[P, R], params P) chromedp.Action[chromedp.Void] {
	return chromedp.Func(func(ctx context.Context, t *chromedp.Target) error {
		_, err := cdp.Call(ctx, t, command, params)
		return err
	})
}

// answerCDPProxyAuth gives Chrome the proxy credentials, which its
// --proxy-server flag cannot carry. With Fetch enabled every request pauses
// until it is continued, so both loops run until the scrape's context ends.
func answerCDPProxyAuth(ctx context.Context, user, pass string) error {
	paused := chromedp.Events(ctx, fetch.RequestPaused)
	challenges := chromedp.Events(ctx, fetch.AuthRequired)

	go func() {
		for ev, err := range paused {
			if err != nil {
				return
			}
			_, _ = chromedp.Call(ctx, fetch.ContinueRequest, fetch.ContinueRequestParams{RequestID: ev.RequestID})
		}
	}()

	go func() {
		answered := make(map[fetch.RequestID]bool)
		for ev, err := range challenges {
			if err != nil {
				return
			}
			response := &fetch.AuthChallengeResponse{Response: fetch.AuthChallengeResponseResponseDefault}
			if ev.AuthChallenge != nil && ev.AuthChallenge.Source == fetch.AuthChallengeSourceProxy {
				// A second challenge for the same request means the proxy
				// rejected the credentials; repeating them would loop until
				// the scrape times out.
				if answered[ev.RequestID] {
					response = &fetch.AuthChallengeResponse{Response: fetch.AuthChallengeResponseResponseCancelAuth}
				} else {
					answered[ev.RequestID] = true
					response = &fetch.AuthChallengeResponse{
						Response: fetch.AuthChallengeResponseResponseProvideCredentials,
						Username: user,
						Password: pass,
					}
				}
			}
			_, _ = chromedp.Call(ctx, fetch.ContinueWithAuth, fetch.ContinueWithAuthParams{
				RequestID:             ev.RequestID,
				AuthChallengeResponse: response,
			})
		}
	}()

	_, err := chromedp.Call(ctx, fetch.Enable, fetch.EnableParams{HandleAuthRequests: new(true)})
	return err
}

// click all xpaths listed in the scraper config
func setCDPClicks(driverOptions scraperDriverOptions) chromedp.Action[chromedp.Void] {
	var steps []chromedp.Action[chromedp.Void]
	for _, click := range driverOptions.Clicks { // for each click element find the node from the xpath and add a click action
		if click.XPath != "" {
			xpath := click.XPath
			waitDuration := scrapeDefaultSleep
			if click.Sleep > 0 {
				waitDuration = time.Duration(click.Sleep) * time.Second
			}

			steps = append(steps, chromedp.Func(func(ctx context.Context, t *chromedp.Target) error {
				nodes, err := chromedp.Nodes(xpath, chromedp.AtLeast(0))(ctx, t)
				if err != nil {
					logger.Debugf("Error %s looking for click xpath %s.\n", err, xpath)
					return err
				}
				if len(nodes) == 0 {
					logger.Debugf("Click xpath %s not found in page.\n", xpath)
					return nil
				}
				logger.Debugf("Clicking %s\n", xpath)
				_, err = chromedp.MouseClickNode(nodes[0])(ctx, t)
				return err
			}), chromedp.Sleep(waitDuration))
		}
	}
	return chromedp.Steps(steps...)
}

// getRemoteCDPWSAddress returns the complete remote address that is required to access the cdp instance
func getRemoteCDPWSAddress(ctx context.Context, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	var json = jsoniter.ConfigCompatibleWithStandardLibrary
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	remote := result["webSocketDebuggerUrl"].(string)
	logger.Debugf("Remote cdp instance found %s", remote)
	return remote, err
}

func cdpHeaders(driverOptions scraperDriverOptions) map[string]interface{} {
	headers := map[string]interface{}{}
	if driverOptions.Headers != nil {
		for _, h := range driverOptions.Headers {
			if h.Key != "" {
				headers[h.Key] = h.Value
				logger.Debugf("[scraper] adding header <%s:%s>", h.Key, h.Value)
			}
		}
	}
	return headers
}

func proxyUsesAuth(proxyUrl string) bool {
	if proxyUrl == "" {
		return false
	}
	matches := proxyAuthRE.FindAllStringSubmatch(proxyUrl, -1)
	if matches != nil {
		split := matches[0]
		return len(split) == 0 || (len(split) > 5 && split[3] != "")
	}

	return false
}

func splitProxyAuth(proxyUrl string) (string, string, string) {
	if proxyUrl == "" {
		return "", "", ""
	}
	matches := proxyAuthRE.FindAllStringSubmatch(proxyUrl, -1)

	if matches != nil && len(matches[0]) > 5 {
		split := matches[0]
		return split[1] + split[5], split[3], split[4]
	}

	return proxyUrl, "", ""
}
