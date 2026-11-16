package ffmpeg

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"

	"github.com/stashapp/stash/pkg/logger"
	"github.com/stashapp/stash/pkg/models"
)

// Keyframe stills and indexes back the coarse scrub tier. Both use the scene's
// primary file. Stills match the 240p scrub stream's dimensions and SDR output.
// See docs/keyframe-previews.md for the HTTP contract.

// Persistent store kinds. The store bounds each kind independently.
const (
	KeyframeStoreIndex = "index"
	KeyframeStoreStill = "still"
)

// KeyframeStoreFile is the persistent store's file name in the cache directory.
const KeyframeStoreFile = "keyframes.sqlite"

// KeyframeStoreBudgets are the persistent byte budgets per kind. A packed
// index costs about two bytes per keyframe (roughly 20 MB for a whole large
// library); a 240p still is typically 5–15 KB.
func KeyframeStoreBudgets() map[string]int64 {
	return map[string]int64{KeyframeStoreIndex: 64 << 20, KeyframeStoreStill: 64 << 20}
}

// KeyframeStore persists disposable keyframe data across restarts. Get returns
// nil without an error for a missing entry; errors are treated as misses.
type KeyframeStore interface {
	Get(ctx context.Context, kind, key string) ([]byte, error)
	Put(ctx context.Context, kind, key string, value []byte) error
	Close() error
}

// keyframeStillRevision identifies the still pipeline (dimensions, tone
// mapping, encoder). A change gives every file a new public version so
// immutable browser copies are not reused.
const keyframeStillRevision = 1

const (
	// One demux scan at a time: it reads the whole file.
	keyframeIndexConcurrency = 1
	// Server-wide concurrent still extractions; more are refused with 503.
	keyframeStillConcurrency = 3
	keyframeIndexTimeout     = 60 * time.Second
	keyframeStillTimeout     = 5 * time.Second
	// A scan continues this long after its last waiter leaves, so the steady
	// stream of still requests during a drag can rejoin it.
	keyframeIndexIdleGrace = 10 * time.Second
	// How long a still request without a cached index waits for one before
	// seeking without it.
	keyframeStillIndexWait = 250 * time.Millisecond
	// Failed scans are not retried for this long.
	keyframeIndexFailureTTL = 10 * time.Minute
	// Bound on how long an index request waits, including its queue time.
	keyframeIndexRequestTimeout = 2 * time.Minute
	// Keyframes are reported within this tolerance of the requested time.
	keyframeTimeToleranceMS = 10
)

var (
	errKeyframeSourceMissing = errors.New("video file is unavailable")
	errKeyframeIndexTimeout  = errors.New("keyframe index timed out")
	errKeyframeIndexStopped  = errors.New("keyframe index work stopped")
	errKeyframeBusy          = errors.New("keyframe extraction is saturated")
)

type keyframeFailure struct {
	err   error
	until time.Time
}

type keyframeIndexJob struct {
	ctx     context.Context
	cancel  context.CancelFunc
	done    chan struct{}
	waiters int
	idle    *time.Timer
	index   *keyframeIndex
	err     error
}

type keyframeService struct {
	ctx     context.Context
	encoder *FFMpeg
	probe   *FFProbe

	storeMu sync.RWMutex
	store   KeyframeStore

	indexes       *lru.Cache[string, *keyframeIndex]
	failures      *lru.Cache[string, keyframeFailure]
	stillWarnings *lru.Cache[string, struct{}]

	mu   sync.Mutex
	jobs map[string]*keyframeIndexJob

	indexSlots chan struct{}
	stillSlots chan struct{}

	indexTimeout   time.Duration
	stillTimeout   time.Duration
	idleGrace      time.Duration
	stillIndexWait time.Duration
	failureTTL     time.Duration
	maxResolution  int
	now            func() time.Time
}

func newKeyframeService(ctx context.Context, encoder *FFMpeg, probe *FFProbe) *keyframeService {
	// At most ~400 KB each (a thinned all-intra index); typically tens of KB.
	indexes, _ := lru.New[string, *keyframeIndex](64)
	failures, _ := lru.New[string, keyframeFailure](1024)
	stillWarnings, _ := lru.New[string, struct{}](256)
	return &keyframeService{
		ctx:            ctx,
		encoder:        encoder,
		probe:          probe,
		indexes:        indexes,
		failures:       failures,
		stillWarnings:  stillWarnings,
		jobs:           map[string]*keyframeIndexJob{},
		indexSlots:     make(chan struct{}, keyframeIndexConcurrency),
		stillSlots:     make(chan struct{}, keyframeStillConcurrency),
		indexTimeout:   keyframeIndexTimeout,
		stillTimeout:   keyframeStillTimeout,
		idleGrace:      keyframeIndexIdleGrace,
		stillIndexWait: keyframeStillIndexWait,
		failureTTL:     keyframeIndexFailureTTL,
		maxResolution:  models.StreamingResolutionEnumLow.GetMaxResolution(),
		now:            time.Now,
	}
}

func (s *keyframeService) setStore(store KeyframeStore) {
	s.storeMu.Lock()
	defer s.storeMu.Unlock()
	s.store = store
}

func (s *keyframeService) getStore() KeyframeStore {
	s.storeMu.RLock()
	defer s.storeMu.RUnlock()
	return s.store
}

func (s *keyframeService) closeStore() {
	s.storeMu.Lock()
	store := s.store
	s.store = nil
	s.storeMu.Unlock()
	if store != nil {
		if err := store.Close(); err != nil {
			logger.Warnf("[keyframes] closing cache: %v", err)
		}
	}
}

// keyframeSource identifies one primary file version.
type keyframeSource struct {
	vf       *models.VideoFile
	path     string
	indexKey string
	version  string
}

// newKeyframeSource derives the cache key from the file record and its live
// size and modification time. File generations advance on observed location
// and byte changes; the live stat also covers changes a scan has not yet seen.
// The public version is a digest, so it does not expose internal file keys.
func newKeyframeSource(vf *models.VideoFile) (*keyframeSource, error) {
	if vf == nil || vf.BaseFile == nil || vf.Path == "" {
		return nil, errKeyframeSourceMissing
	}
	info, err := os.Stat(vf.Path)
	if err != nil || !info.Mode().IsRegular() {
		return nil, errKeyframeSourceMissing
	}
	indexKey := fmt.Sprintf("r%d:%d:%d:%d:%d", keyframeIndexRevision, vf.ID, vf.Generation, info.Size(), info.ModTime().UnixNano())
	transfer := colorMetadataFromModel(vf).Transfer
	sum := sha256.Sum256(fmt.Appendf(nil, "stash-keyframes|%d|%s|%s", keyframeStillRevision, indexKey, transfer))
	return &keyframeSource{vf: vf, path: vf.Path, indexKey: indexKey, version: hex.EncodeToString(sum[:10])}, nil
}

// cachedIndex returns an index from memory or the persistent store without
// starting a scan.
func (s *keyframeService) cachedIndex(ctx context.Context, key string) *keyframeIndex {
	if index, ok := s.indexes.Get(key); ok {
		return index
	}
	store := s.getStore()
	if store == nil {
		return nil
	}
	data, err := store.Get(ctx, KeyframeStoreIndex, key)
	if err != nil {
		if ctx.Err() == nil {
			logger.Debugf("[keyframes] reading cached index: %v", err)
		}
		return nil
	}
	if data == nil {
		return nil
	}
	index, err := decodeKeyframeIndex(data)
	if err != nil {
		logger.Debugf("[keyframes] discarding cached index: %v", err)
		return nil
	}
	s.indexes.Add(key, index)
	return index
}

// index returns the file's keyframe index, waiting for a shared scan. A scan
// is cancelled once nobody has waited for it for idleGrace.
func (s *keyframeService) index(ctx context.Context, src *keyframeSource) (*keyframeIndex, error) {
	if index := s.cachedIndex(ctx, src.indexKey); index != nil {
		return index, nil
	}
	if failure, ok := s.failures.Get(src.indexKey); ok && s.now().Before(failure.until) {
		return nil, failure.err
	}
	job := s.attach(src)
	defer s.detach(job)
	select {
	case <-job.done:
		return job.index, job.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (s *keyframeService) attach(src *keyframeSource) *keyframeIndexJob {
	s.mu.Lock()
	defer s.mu.Unlock()
	job := s.jobs[src.indexKey]
	// An abandoned scan can still be unwinding; start a fresh one.
	if job == nil || job.ctx.Err() != nil {
		ctx, cancel := context.WithCancel(s.ctx)
		job = &keyframeIndexJob{ctx: ctx, cancel: cancel, done: make(chan struct{})}
		s.jobs[src.indexKey] = job
		go s.run(job, src)
	}
	job.waiters++
	if job.idle != nil {
		job.idle.Stop()
		job.idle = nil
	}
	return job
}

func (s *keyframeService) detach(job *keyframeIndexJob) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job.waiters--
	if job.waiters > 0 || job.idle != nil {
		return
	}
	select {
	case <-job.done:
		return
	default:
	}
	job.idle = time.AfterFunc(s.idleGrace, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if job.waiters == 0 {
			job.cancel()
		}
	})
}

func (s *keyframeService) run(job *keyframeIndexJob, src *keyframeSource) {
	defer func() {
		s.mu.Lock()
		if s.jobs[src.indexKey] == job {
			delete(s.jobs, src.indexKey)
		}
		if job.idle != nil {
			job.idle.Stop()
			job.idle = nil
		}
		s.mu.Unlock()
		close(job.done)
		job.cancel()
	}()
	// A concurrent request or an earlier process may already have stored it.
	if index := s.cachedIndex(job.ctx, src.indexKey); index != nil {
		job.index = index
		return
	}
	select {
	case s.indexSlots <- struct{}{}:
		defer func() { <-s.indexSlots }()
	case <-job.ctx.Done():
		job.err = errKeyframeIndexStopped
		return
	}
	if s.probe == nil {
		job.err = errors.New("ffprobe is unavailable")
		return
	}
	started := time.Now()
	index, err := s.probe.scanKeyframes(job.ctx, src.path, s.indexTimeout)
	elapsed := time.Since(started)
	if err != nil {
		switch {
		case job.ctx.Err() != nil:
			// Abandoned or shutting down: not a property of the file.
			job.err = errKeyframeIndexStopped
			return
		case errors.Is(err, context.DeadlineExceeded):
			job.err = errKeyframeIndexTimeout
		default:
			job.err = fmt.Errorf("%w: %w", errNoKeyframes, err)
		}
		s.failures.Add(src.indexKey, keyframeFailure{err: job.err, until: s.now().Add(s.failureTTL)})
		logger.Warnf("[keyframes] indexing %s failed after %d ms: %v", src.path, elapsed.Milliseconds(), err)
		return
	}
	logger.Debugf("[keyframes] indexed %d keyframes of %s in %d ms", len(index.times), src.path, elapsed.Milliseconds())
	job.index = index
	s.indexes.Add(src.indexKey, index)
	if store := s.getStore(); store != nil {
		if err := store.Put(job.ctx, KeyframeStoreIndex, src.indexKey, encodeKeyframeIndex(index)); err != nil {
			logger.Debugf("[keyframes] caching index: %v", err)
		}
	}
}

type keyframeStillResult struct {
	jpeg []byte
	ms   int64
	// canonical results were resolved through the index: ms is an index time
	// and the image is the one every request for that time returns.
	canonical bool
}

// still returns the keyframe at or before t (+10 ms).
func (s *keyframeService) still(ctx context.Context, src *keyframeSource, t float64) (*keyframeStillResult, error) {
	limit := int64(math.Round(t*1000)) + keyframeTimeToleranceMS
	index := s.cachedIndex(ctx, src.indexKey)
	if index == nil {
		// Short files index in tens of milliseconds. Otherwise keep the scan
		// running for later requests and seek without it now.
		wait, cancel := context.WithTimeout(ctx, s.stillIndexWait)
		index, _ = s.index(wait, src)
		cancel()
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
	color := colorMetadataFromModel(src.vf)
	if index != nil {
		color = mergeProbedColor(color, index.color)
	}
	filter := keyframeStillFilter(src.vf, color, s.maxResolution)

	var target int64
	stillKey := ""
	store := s.getStore()
	if index != nil {
		target = index.atOrBefore(limit)
		stillKey = src.version + "@" + strconv.FormatInt(target, 10)
		if store != nil {
			if data, err := store.Get(ctx, KeyframeStoreStill, stillKey); err == nil && data != nil {
				return &keyframeStillResult{jpeg: data, ms: target, canonical: true}, nil
			}
		}
	}

	select {
	case s.stillSlots <- struct{}{}:
		defer func() { <-s.stillSlots }()
	default:
		return nil, errKeyframeBusy
	}
	if s.encoder == nil {
		return nil, errors.New("ffmpeg is unavailable")
	}

	var still *keyframeStill
	var err error
	if index != nil {
		// Seek accurately to just before the indexed keyframe: ffmpeg discards
		// earlier keyframes, whatever the demuxer's seek heuristics chose.
		still, err = s.encoder.extractKeyframeStill(ctx, src.path, float64(target)/1000-0.001, true, filter, s.stillTimeout)
	} else {
		// Without an index, report the keyframe the demuxer seeks to. Some
		// containers choose an earlier one, so this result is not canonical.
		still, err = s.encoder.extractKeyframeStill(ctx, src.path, float64(limit)/1000, false, filter, s.stillTimeout)
	}
	if err != nil {
		return nil, err
	}
	actual := int64(math.Round(still.time * 1000))
	logger.Debugf("[keyframes] still of %s at %.3fs: frame %.3fs, ffmpeg %d ms, %d bytes", src.path, t, float64(actual)/1000, still.elapsed.Milliseconds(), len(still.jpeg))
	result := &keyframeStillResult{jpeg: still.jpeg, ms: actual}
	if index != nil {
		if diff := actual - target; diff >= -2 && diff <= 2 {
			result.ms, result.canonical = target, true
			if store != nil {
				if err := store.Put(ctx, KeyframeStoreStill, stillKey, still.jpeg); err != nil && ctx.Err() == nil {
					logger.Debugf("[keyframes] caching still: %v", err)
				}
			}
		} else {
			logger.Debugf("[keyframes] %s decoded %.3fs for indexed keyframe %.3fs", src.path, float64(actual)/1000, float64(target)/1000)
		}
	}
	return result, nil
}

// mergeProbedColor prefers tags observed in the file, as the scrub stream does.
func mergeProbedColor(color, probed videoColorMetadata) videoColorMetadata {
	if probed.Range != "" {
		color.Range = probed.Range
	}
	if probed.Space != "" {
		color.Space = probed.Space
	}
	if probed.Transfer != "" {
		color.Transfer = probed.Transfer
	}
	if probed.Primaries != "" {
		color.Primaries = probed.Primaries
	}
	return color
}

type keyframeIndexResponse struct {
	Version  string    `json:"version"`
	Duration float64   `json:"duration"`
	Times    []float64 `json:"times"`
}

func formatKeyframeTime(ms int64) string {
	return strconv.FormatFloat(float64(ms)/1000, 'f', 3, 64)
}

// ServeKeyframeIndex serves the primary file's keyframe index as JSON.
func (sm *StreamManager) ServeKeyframeIndex(w http.ResponseWriter, r *http.Request, vf *models.VideoFile) {
	if sm.keyframes == nil {
		http.Error(w, "keyframe previews unavailable", http.StatusServiceUnavailable)
		return
	}
	src, err := newKeyframeSource(vf)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	etag := `"` + src.version + `"`
	w.Header().Set("Cache-Control", "private, no-cache")
	w.Header().Set("ETag", etag)
	if etagMatches(r.Header.Get("If-None-Match"), etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), keyframeIndexRequestTimeout)
	defer cancel()
	index, err := sm.keyframes.index(ctx, src)
	if err != nil {
		w.Header().Del("ETag")
		switch {
		case r.Context().Err() != nil:
			return
		case errors.Is(err, errNoKeyframes):
			http.Error(w, "keyframe index unavailable", http.StatusNotFound)
		default:
			// Timed out, still queued, or stopped: the client may retry later.
			retry := 30
			if errors.Is(err, errKeyframeIndexTimeout) {
				retry = int(sm.keyframes.failureTTL / time.Second)
			}
			w.Header().Set("Retry-After", strconv.Itoa(retry))
			http.Error(w, "keyframe index unavailable", http.StatusServiceUnavailable)
		}
		return
	}
	response := keyframeIndexResponse{
		Version:  src.version,
		Duration: math.Round(vf.Duration*1000) / 1000,
		Times:    make([]float64, len(index.times)),
	}
	for i, ms := range index.times {
		response.Times[i] = float64(ms) / 1000
	}
	body, err := json.Marshal(response)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	_, _ = w.Write(body)
}

// ServeKeyframe serves a JPEG of the keyframe at or before ?t= (+10 ms).
func (sm *StreamManager) ServeKeyframe(w http.ResponseWriter, r *http.Request, vf *models.VideoFile) {
	if sm.keyframes == nil {
		http.Error(w, "keyframe previews unavailable", http.StatusServiceUnavailable)
		return
	}
	query := r.URL.Query()
	t, err := parseKeyframeRequestTime(query.Get("t"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	src, err := newKeyframeSource(vf)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	result, err := sm.keyframes.still(r.Context(), src, t)
	if err != nil {
		switch {
		case r.Context().Err() != nil:
			// The client went away; ffmpeg has already been stopped.
		case errors.Is(err, errKeyframeBusy):
			w.Header().Set("Retry-After", "1")
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
		default:
			// A drag over an undecodable file produces many requests: warn once.
			if previous, _ := sm.keyframes.stillWarnings.ContainsOrAdd(src.version, struct{}{}); previous {
				logger.Debugf("[keyframes] still of %s at %.3fs failed: %v", src.path, t, err)
			} else {
				logger.Warnf("[keyframes] still of %s at %.3fs failed: %v", src.path, t, err)
			}
			http.Error(w, "keyframe extraction failed", http.StatusInternalServerError)
		}
		return
	}
	// Only an index-resolved frame requested with the current version is the
	// immutable content of this URL.
	if result.canonical && query.Get("v") == src.version {
		w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "no-cache")
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Content-Length", strconv.Itoa(len(result.jpeg)))
	w.Header().Set("X-Keyframe-Time", formatKeyframeTime(result.ms))
	w.Header().Set("Access-Control-Expose-Headers", "X-Keyframe-Time")
	_, _ = w.Write(result.jpeg)
}

func parseKeyframeRequestTime(value string) (float64, error) {
	if value == "" {
		return 0, errors.New("t is required")
	}
	t, err := strconv.ParseFloat(value, 64)
	if err != nil || math.IsNaN(t) || math.IsInf(t, 0) || t < 0 || t > 1e7 {
		return 0, errors.New("t must be a non-negative number of seconds")
	}
	return t, nil
}

func etagMatches(header, etag string) bool {
	for _, candidate := range strings.Split(header, ",") {
		candidate = strings.TrimPrefix(strings.TrimSpace(candidate), "W/")
		if candidate == etag || candidate == "*" {
			return true
		}
	}
	return false
}
