package api

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"github.com/stashapp/stash/pkg/entityid"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	gqlHandler "github.com/99designs/gqlgen/graphql/handler"
	gqlExtension "github.com/99designs/gqlgen/graphql/handler/extension"
	gqlLru "github.com/99designs/gqlgen/graphql/handler/lru"
	gqlTransport "github.com/99designs/gqlgen/graphql/handler/transport"
	gqlPlayground "github.com/99designs/gqlgen/graphql/playground"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/go-chi/httplog"
	"github.com/vearutop/statigz"
	"github.com/vektah/gqlparser/v2/ast"

	"github.com/stashapp/stash/internal/api/loaders"
	"github.com/stashapp/stash/internal/build"
	"github.com/stashapp/stash/internal/manager"
	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/internal/sharing"
	"github.com/stashapp/stash/pkg/fsutil"
	"github.com/stashapp/stash/pkg/logger"
	"github.com/stashapp/stash/pkg/plugin"
	"github.com/stashapp/stash/pkg/utils"
	"github.com/stashapp/stash/ui"
)

const (
	loginEndpoint       = "/login"
	loginLocaleEndpoint = loginEndpoint + "/locale"
	logoutEndpoint      = "/logout"
	gqlEndpoint         = "/graphql"
	playgroundEndpoint  = "/playground"
)

type Server struct {
	http.Server
	requestCancel  context.CancelFunc
	displayAddress string

	manager                *manager.Manager
	shares                 *sharing.Service
	ingestWorker           *archiveWorkerRuntime
	albumWorker            *archiveWorkerRuntime
	postMergeNotifications *archiveWorkerRuntime
	translationWorker      *archiveWorkerRuntime
	enrichmentMaintenance  *archiveWorkerRuntime
	discoveryMaintenance   *archiveWorkerRuntime
	discoveryComparison    *archiveWorkerRuntime
	discoveryPublication   *archiveWorkerRuntime
}

// TODO - os.DirFS doesn't implement ReadDir, so re-implement it here
// This can be removed when we upgrade go
type osFS string

func (dir osFS) ReadDir(name string) ([]os.DirEntry, error) {
	fullname := string(dir) + "/" + name
	entries, err := os.ReadDir(fullname)
	if err != nil {
		var e *os.PathError
		if errors.As(err, &e) {
			// See comment in dirFS.Open.
			e.Path = name
		}
		return nil, err
	}
	return entries, nil
}

func (dir osFS) Open(name string) (fs.File, error) {
	return os.DirFS(string(dir)).Open(name)
}

type reloadableStatigzServer struct {
	fs fs.ReadDirFS

	mutex        sync.Mutex
	server       *statigz.Server
	indexModTime time.Time
	indexSize    int64
}

func newReloadableStatigzServer(fsys fs.ReadDirFS) *reloadableStatigzServer {
	return &reloadableStatigzServer{
		fs: fsys,
	}
}

func (s *reloadableStatigzServer) indexSignature() (time.Time, int64) {
	info, err := fs.Stat(s.fs, "index.html")
	if err != nil {
		return time.Time{}, -1
	}

	return info.ModTime(), info.Size()
}

func (s *reloadableStatigzServer) currentServer(forceReload bool) *statigz.Server {
	indexModTime, indexSize := s.indexSignature()

	s.mutex.Lock()
	defer s.mutex.Unlock()

	if s.server == nil || forceReload || !s.indexModTime.Equal(indexModTime) || s.indexSize != indexSize {
		s.server = statigz.FileServer(s.fs)
		s.indexModTime = indexModTime
		s.indexSize = indexSize
	}

	return s.server
}

func (s *reloadableStatigzServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	server := s.currentServer(false)
	if !server.Found(r) {
		server = s.currentServer(true)
	}

	server.ServeHTTP(w, r)
}

// Initialize creates a new [Server] instance.
// It assumes that the [manager.Manager] instance has been initialised.
func Initialize() (*Server, error) {
	mgr := manager.GetInstance()
	cfg := mgr.Config

	initCustomPerformerImages(cfg.GetCustomPerformerImageLocation())

	displayHost := cfg.GetHost()
	if displayHost == "0.0.0.0" {
		displayHost = "localhost"
	}
	displayAddress := displayHost + ":" + strconv.Itoa(cfg.GetPort())

	address := cfg.GetHost() + ":" + strconv.Itoa(cfg.GetPort())
	tlsConfig, err := makeTLSConfig(cfg)
	if err != nil {
		// assume we don't want to start with a broken TLS configuration
		return nil, fmt.Errorf("error loading TLS config: %v", err)
	}

	if tlsConfig != nil {
		displayAddress = "https://" + displayAddress + "/"
	} else {
		displayAddress = "http://" + displayAddress + "/"
	}

	r := chi.NewRouter()

	requestContext, requestCancel := context.WithCancel(context.Background())
	server := &Server{
		requestCancel: requestCancel,
		Server: http.Server{
			BaseContext: func(net.Listener) context.Context { return requestContext },
			Addr:        address,
			Handler:     r,
			TLSConfig:   tlsConfig,
			// disable http/2 support by default
			// when http/2 is enabled, we are unable to hijack and close
			// the connection/request. This is necessary to stop running
			// streams when deleting a scene file.
			TLSNextProto: make(map[string]func(*http.Server, *tls.Conn, http.Handler)),
		},
		displayAddress: displayAddress,
		manager:        mgr,
		shares:         sharing.New(mgr.Repository),
	}

	r.Use(middleware.Heartbeat("/healthz"))
	r.Use(cors.AllowAll().Handler)
	r.Use(RequestIPMiddleware)
	r.Use(authenticateHandler())
	visitedPluginHandler := mgr.SessionStore.VisitedPluginHandler()
	r.Use(visitedPluginHandler)

	r.Use(middleware.Recoverer)

	if cfg.GetLogAccess() {
		httpLogger := httplog.NewLogger("Stash", httplog.Options{
			Concise: true,
		})
		r.Use(httplog.RequestLogger(httpLogger))
	}
	r.Use(SecurityHeadersMiddleware)
	r.Use(middleware.Compress(4))
	r.Use(middleware.StripSlashes)
	r.Use(BaseURLMiddleware)

	recoverFunc := func(ctx context.Context, err interface{}) error {
		logger.Error(err)
		debug.PrintStack()

		message := fmt.Sprintf("Internal system error. Error <%v>", err)
		return errors.New(message)
	}

	repo := mgr.Repository

	dataloaders := loaders.Middleware{
		Repository: repo,
	}

	r.Use(dataloaders.Middleware)
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(entityid.WithRepository(r.Context(), repo)))
		})
	})

	pluginCache := mgr.PluginCache
	sceneService := mgr.SceneService
	imageService := mgr.ImageService
	galleryService := mgr.GalleryService
	groupService := mgr.GroupService
	sourceTracking := cfg.GetSourceTrackingEnabled()
	resolver := &Resolver{
		settings:             &settingsService{config: cfg, runtime: mgr, setLogLevel: mgr.Logger.SetLogLevel},
		sourceTrackingActive: sourceTracking,
		shares:               server.shares,
		scraperCacheStore:    mgr.ScraperCache,
		repository:           repo,
		sceneService:         sceneService,
		imageService:         imageService,
		galleryService:       galleryService,
		groupService:         groupService,
		bulkUpdater:          mgr,
		hookExecutor:         pluginCache,
	}

	gqlSrv := gqlHandler.New(newLibrarySchema(Config{Resolvers: resolver}))
	gqlSrv.SetRecoverFunc(recoverFunc)
	gqlSrv.AddTransport(graphQLWebsocketTransport())
	gqlSrv.AddTransport(gqlTransport.Options{})
	gqlSrv.AddTransport(gqlTransport.GET{})
	gqlSrv.AddTransport(gqlTransport.POST{})
	gqlSrv.AddTransport(gqlTransport.MultipartForm{
		MaxUploadSize: cfg.GetMaxUploadSize(),
	})

	gqlSrv.SetQueryCache(gqlLru.New[*ast.QueryDocument](1000))
	gqlSrv.Use(gqlExtension.Introspection{})

	gqlSrv.SetErrorPresenter(gqlErrorHandler)

	gqlHandlerFunc := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		gqlSrv.ServeHTTP(w, r)
	}

	// register GQL handler with plugin cache
	// chain the visited plugin handler
	// also requires the dataloader middleware
	gqlHandler := visitedPluginHandler(dataloaders.Middleware(http.HandlerFunc(gqlHandlerFunc)))
	pluginCache.RegisterGQLHandler(gqlHandler)

	r.HandleFunc(gqlEndpoint, gqlHandlerFunc)
	r.HandleFunc(playgroundEndpoint, func(w http.ResponseWriter, r *http.Request) {
		setPageSecurityHeaders(w, r, pluginCache.ListPlugins())
		endpoint := getProxyPrefix(r) + gqlEndpoint
		gqlPlayground.Handler("GraphQL playground", endpoint, gqlPlayground.WithGraphiqlEnablePluginExplorer(true))(w, r)
	})

	ingestion, archiveRoutes := server.configureSourceTracking(sourceTracking)
	if ingestion != nil {
		r.Mount("/api/v3/ingest-admin", (&ingestRoutes{service: ingestion}).adminRouter())
	} else {
		r.Mount("/api/v3/ingest-admin", http.HandlerFunc(sourceTrackingDisabled))
	}
	r.Mount("/api/v3/backups", (&nativeBackupRoutes{manager: mgr}).router())
	r.Mount("/api/v3/archive", archiveRoutes.routerWithSourceTracking(sourceTracking))
	r.Mount("/performer", server.getPerformerRoutes())
	r.Mount("/scene", server.getSceneRoutes())
	r.Mount("/gallery", server.getGalleryRoutes())
	r.Mount("/image", server.getImageRoutes())
	r.Mount("/studio", server.getStudioRoutes())
	r.Mount("/group", server.getGroupRoutes())
	r.Mount("/tag", server.getTagRoutes())
	r.Mount("/downloads", server.getDownloadsRoutes())
	r.Mount("/plugin", server.getPluginRoutes())

	r.HandleFunc("/css", cssHandler(cfg))
	r.HandleFunc("/javascript", javascriptHandler(cfg))
	r.HandleFunc("/customlocales", customLocalesHandler(cfg))

	staticLoginUI := statigz.FileServer(ui.LoginUIBox.(fs.ReadDirFS))

	r.Get(loginEndpoint, handleLogin())
	r.Post(loginEndpoint, handleLoginPost())
	r.Get(logoutEndpoint, handleLogout())
	r.Get(loginLocaleEndpoint, handleLoginLocale(cfg))
	r.HandleFunc(loginEndpoint+"/*", func(w http.ResponseWriter, r *http.Request) {
		r.URL.Path = strings.TrimPrefix(r.URL.Path, loginEndpoint)
		w.Header().Set("Cache-Control", "no-cache")
		staticLoginUI.ServeHTTP(w, r)
	})

	// Serve static folders
	customServedFolders := cfg.GetCustomServedFolders()
	if customServedFolders != nil {
		r.Mount("/custom", getCustomRoutes(customServedFolders))
	}

	var uiFS fs.FS
	var staticUI http.Handler
	customUILocation := cfg.GetUILocation()
	if customUILocation != "" {
		logger.Debugf("Serving UI from %s", customUILocation)
		uiFS = osFS(customUILocation)
		staticUI = newReloadableStatigzServer(uiFS.(fs.ReadDirFS))
	} else {
		logger.Debug("Serving embedded v3 UI")
		uiFS = ui.UIBox
		staticUI = statigz.FileServer(uiFS.(fs.ReadDirFS))
	}

	// handle favicon override
	r.HandleFunc("/favicon.ico", handleFavicon(staticUI))

	// Serve the web app
	r.HandleFunc("/*", func(w http.ResponseWriter, r *http.Request) {
		ext := path.Ext(r.URL.Path)

		if ext == ".html" || ext == "" {
			w.Header().Set("Content-Type", "text/html")
			setPageSecurityHeaders(w, r, pluginCache.ListPlugins())
		}

		if ext == "" || r.URL.Path == "/" || r.URL.Path == "/index.html" {
			themeColor := cfg.GetThemeColor()
			data, err := fs.ReadFile(uiFS, "index.html")
			if err != nil {
				panic(err)
			}
			indexHtml := string(data)

			prefix := getProxyPrefix(r)
			indexHtml = strings.ReplaceAll(indexHtml, "%COLOR%", themeColor)
			indexHtml = strings.Replace(indexHtml, `<base href="/"`, fmt.Sprintf(`<base href="%s/"`, prefix), 1)

			utils.ServeStaticContent(w, r, []byte(indexHtml))
		} else {
			isStatic, _ := path.Match("/assets/*", r.URL.Path)
			if isStatic {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			} else {
				w.Header().Set("Cache-Control", "no-cache")
			}

			staticUI.ServeHTTP(w, r)
		}
	})

	logger.Infof("stash version: %s", build.VersionString())
	server.Handler = server.withShareRoutes(r)
	server.Handler = withIngestRoutes(server.Handler, ingestion, server.ingestWorker != nil)
	go printLatestVersion(context.TODO())

	return server, nil
}

func handleFavicon(staticUI http.Handler) func(w http.ResponseWriter, r *http.Request) {
	mgr := manager.GetInstance()
	cfg := mgr.Config

	// check if favicon.ico exists in the config directory
	// if so, use that
	// otherwise, use the embedded one
	iconPath := filepath.Join(cfg.GetConfigPath(), "favicon.ico")
	exists, _ := fsutil.FileExists(iconPath)

	if exists {
		logger.Debugf("Using custom favicon at %s", iconPath)
	}

	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")

		if exists {
			http.ServeFile(w, r, iconPath)
		} else {
			staticUI.ServeHTTP(w, r)
		}
	}
}

// Start starts the server. It listens on the configured address and port.
// It calls ListenAndServeTLS if TLS is configured, otherwise it calls ListenAndServe.
// Calls to Start are blocked until the server is shutdown.
func (s *Server) Start() error {
	s.ingestWorker.start()
	s.albumWorker.start()
	s.postMergeNotifications.start()
	s.translationWorker.start()
	s.enrichmentMaintenance.start()
	s.discoveryMaintenance.start()
	s.discoveryComparison.start()
	s.discoveryPublication.start()
	defer s.ingestWorker.stop()
	defer s.albumWorker.stop()
	defer s.postMergeNotifications.stop()
	defer s.translationWorker.stop()
	defer s.enrichmentMaintenance.stop()
	defer s.discoveryMaintenance.stop()
	defer s.discoveryComparison.stop()
	defer s.discoveryPublication.stop()
	logger.Infof("stash is listening on " + s.Addr)
	logger.Infof("stash is running at " + s.displayAddress)

	if s.TLSConfig != nil {
		return s.ListenAndServeTLS("", "")
	} else {
		return s.ListenAndServe()
	}
}

// Shutdown cancels active work and drains the server within the default deadline.
func (s *Server) Shutdown() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return s.ShutdownContext(ctx)
}

// ShutdownContext cancels workers concurrently with HTTP draining. The caller
// must keep the database open if any active work exceeds the shutdown deadline.
func (s *Server) ShutdownContext(ctx context.Context) error {
	// Also notify upgraded WebSocket subscriptions, which http.Shutdown does not
	// drain as ordinary HTTP requests.
	if s.requestCancel != nil {
		s.requestCancel()
	}
	workers := []*archiveWorkerRuntime{s.ingestWorker, s.albumWorker, s.postMergeNotifications,
		s.translationWorker, s.enrichmentMaintenance, s.discoveryMaintenance, s.discoveryComparison, s.discoveryPublication}
	results := make(chan error, len(workers))
	for _, worker := range workers {
		go func() { results <- worker.shutdown(ctx) }()
	}
	err := s.Server.Shutdown(ctx)
	if err != nil {
		err = errors.Join(err, s.Server.Close())
	}
	for range workers {
		err = errors.Join(err, <-results)
	}
	return err
}

func (s *Server) getPerformerRoutes() chi.Router {
	repo := s.manager.Repository
	return performerRoutes{
		routes:          routes{txnManager: repo.TxnManager},
		performerFinder: repo.Performer,
		sfwConfig:       s.manager.Config,
	}.Routes()
}

func (s *Server) getSceneRoutes() chi.Router {
	repo := s.manager.Repository
	return sceneRoutes{
		routes:            routes{txnManager: repo.TxnManager},
		sceneFinder:       repo.Scene,
		fileGetter:        repo.File,
		captionFinder:     repo.File,
		sceneMarkerFinder: repo.SceneMarker,
		tagFinder:         repo.Tag,
	}.Routes()
}

func (s *Server) getGalleryRoutes() chi.Router {
	repo := s.manager.Repository
	return galleryRoutes{
		routes:        routes{txnManager: repo.TxnManager},
		repository:    repo,
		imageFinder:   repo.Image,
		galleryFinder: repo.Gallery,
		fileGetter:    repo.File,
	}.Routes()
}

func (s *Server) getImageRoutes() chi.Router {
	repo := s.manager.Repository
	return imageRoutes{
		routes:      routes{txnManager: repo.TxnManager},
		imageFinder: repo.Image,
		fileGetter:  repo.File,
	}.Routes()
}

func (s *Server) getStudioRoutes() chi.Router {
	repo := s.manager.Repository
	return studioRoutes{
		routes:       routes{txnManager: repo.TxnManager},
		studioFinder: repo.Studio,
	}.Routes()
}

func (s *Server) getGroupRoutes() chi.Router {
	repo := s.manager.Repository
	return groupRoutes{
		routes:      routes{txnManager: repo.TxnManager},
		groupFinder: repo.Group,
	}.Routes()
}

func (s *Server) getTagRoutes() chi.Router {
	repo := s.manager.Repository
	return tagRoutes{
		routes:    routes{txnManager: repo.TxnManager},
		tagFinder: repo.Tag,
	}.Routes()
}

func (s *Server) getDownloadsRoutes() chi.Router {
	return downloadsRoutes{}.Routes()
}

func (s *Server) getPluginRoutes() chi.Router {
	return pluginRoutes{
		pluginCache: s.manager.PluginCache,
	}.Routes()
}

func cssHandler(c *config.Config) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		cfg := c.Snapshot()
		var content string
		if cfg.GetCSSEnabled() && !cfg.GetDisableCustomizations() {
			content = cfg.GetCSS()
		}
		w.Header().Set("Content-Type", "text/css")
		utils.ServeStaticContent(w, r, []byte(content))
	}
}

func javascriptHandler(c *config.Config) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		cfg := c.Snapshot()
		var content string
		if cfg.GetJavascriptEnabled() && !cfg.GetDisableCustomizations() {
			content = cfg.GetJavascript()
		}
		w.Header().Set("Content-Type", "text/javascript")
		utils.ServeStaticContent(w, r, []byte(content))
	}
}

func customLocalesHandler(c *config.Config) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		cfg := c.Snapshot()
		content := ""
		if cfg.GetCustomLocalesEnabled() && !cfg.GetDisableCustomizations() {
			content = cfg.GetCustomLocales()
		}
		if content == "" {
			content = "{}"
		}
		w.Header().Set("Content-Type", "application/json")
		utils.ServeStaticContent(w, r, []byte(content))
	}
}

func makeTLSConfig(c *config.Config) (*tls.Config, error) {
	c.InitTLS()
	certFile, keyFile := c.GetTLSFiles()

	if certFile == "" && keyFile == "" {
		// assume http configuration
		return nil, nil
	}

	// ensure both files are present
	if certFile == "" {
		return nil, errors.New("SSL certificate file must be present if key file is present")
	}

	if keyFile == "" {
		return nil, errors.New("SSL key file must be present if certificate file is present")
	}

	cert, err := os.ReadFile(certFile)
	if err != nil {
		return nil, fmt.Errorf("error reading SSL certificate file %s: %v", certFile, err)
	}

	key, err := os.ReadFile(keyFile)
	if err != nil {
		return nil, fmt.Errorf("error reading SSL key file %s: %v", keyFile, err)
	}

	certs := make([]tls.Certificate, 1)
	certs[0], err = tls.X509KeyPair(cert, key)
	if err != nil {
		return nil, fmt.Errorf("error parsing key pair: %v", err)
	}
	tlsConfig := &tls.Config{
		Certificates: certs,
	}

	return tlsConfig, nil
}

func setPageSecurityHeaders(w http.ResponseWriter, r *http.Request, plugins []*plugin.Plugin) {
	c := config.GetInstance()

	defaultSrc := "data: 'self' 'unsafe-inline'"
	connectSrcSlice := []string{
		"data:",
		"'self'",
	}
	imageSrc := "data: *"
	scriptSrcSlice := []string{
		"'self'",
		"http://www.gstatic.com",
		"https://www.gstatic.com",
		"'unsafe-inline'",
		"'unsafe-eval'",
	}
	styleSrcSlice := []string{
		"'self'",
		"'unsafe-inline'",
	}
	mediaSrc := "blob: 'self'"

	// Workaround Safari bug https://bugs.webkit.org/show_bug.cgi?id=201591
	// Allows websocket requests to any origin
	connectSrcSlice = append(connectSrcSlice, "ws:", "wss:")

	// The graphql playground pulls its frontend from a cdn
	if r.URL.Path == playgroundEndpoint {
		connectSrcSlice = append(connectSrcSlice, "https://cdn.jsdelivr.net")
		scriptSrcSlice = append(scriptSrcSlice, "https://cdn.jsdelivr.net")
		styleSrcSlice = append(styleSrcSlice, "https://cdn.jsdelivr.net")
	}

	if !c.IsNewSystem() && c.GetHandyKey() != "" {
		connectSrcSlice = append(connectSrcSlice, "https://www.handyfeeling.com")
	}

	for _, plugin := range plugins {
		if !plugin.Enabled {
			continue
		}

		ui := plugin.UI

		connectSrcSlice = append(connectSrcSlice, ui.CSP.ConnectSrc...)
		scriptSrcSlice = append(scriptSrcSlice, ui.CSP.ScriptSrc...)
		styleSrcSlice = append(styleSrcSlice, ui.CSP.StyleSrc...)
	}

	connectSrc := strings.Join(connectSrcSlice, " ")
	scriptSrc := strings.Join(scriptSrcSlice, " ")
	styleSrc := strings.Join(styleSrcSlice, " ")
	// The offline library registers a bundled, same-origin service worker.
	workerSrc := "blob: 'self'"

	cspDirectives := fmt.Sprintf("default-src %s; connect-src %s; img-src %s; script-src %s; style-src %s; media-src %s;", defaultSrc, connectSrc, imageSrc, scriptSrc, styleSrc, mediaSrc)
	cspDirectives += fmt.Sprintf(" worker-src %s; child-src 'none'; object-src 'none'; form-action 'self';", workerSrc)

	w.Header().Set("Referrer-Policy", "same-origin")
	w.Header().Set("Content-Security-Policy", cspDirectives)
}

func SecurityHeadersMiddleware(next http.Handler) http.Handler {
	fn := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")

		next.ServeHTTP(w, r)
	}
	return http.HandlerFunc(fn)
}

type contextKey struct {
	name string
}

var (
	BaseURLCtxKey = &contextKey{"BaseURL"}
	IPCtxKey      = &contextKey{"requestIP"}
)

func BaseURLMiddleware(next http.Handler) http.Handler {
	fn := func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		scheme := "http"
		if strings.Compare("https", r.URL.Scheme) == 0 || r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
			scheme = "https"
		}
		prefix := getProxyPrefix(r)

		baseURL := scheme + "://" + r.Host + prefix

		externalHost := config.GetInstance().GetExternalHost()
		if externalHost != "" {
			baseURL = externalHost + prefix
		}

		r = r.WithContext(context.WithValue(ctx, BaseURLCtxKey, baseURL))

		next.ServeHTTP(w, r)
	}
	return http.HandlerFunc(fn)
}

func getProxyPrefix(r *http.Request) string {
	return strings.TrimRight(r.Header.Get("X-Forwarded-Prefix"), "/")
}
