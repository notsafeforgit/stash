package ffmpeg

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/stashapp/stash/pkg/fsutil"
	"github.com/stashapp/stash/pkg/logger"
	"github.com/stashapp/stash/pkg/models"
)

const (
	MimeWebmVideo string = "video/webm"
	MimeWebmAudio string = "audio/webm"
	MimeMkvVideo  string = "video/x-matroska"
	MimeMkvAudio  string = "audio/x-matroska"
	MimeMp4Video  string = "video/mp4"
	MimeMp4Audio  string = "audio/mp4"
)

type StreamManager struct {
	cacheDir string
	encoder  *FFMpeg
	ffprobe  *FFProbe

	config      StreamManagerConfig
	lockManager *fsutil.ReadLockManager

	context      context.Context
	cancelFunc   context.CancelFunc
	shutdownOnce sync.Once
	shutdownDone chan struct{}

	runningStreams   map[string]*runningStream
	v3RunningStreams map[string]*v3RunningStream
	v3ClosedSessions map[v3StreamSessionKey]time.Time
	streamsMutex     sync.Mutex
	downloadProgress downloadProgressStore
}

type StreamManagerConfig interface {
	GetMaxStreamingTranscodeSize() models.StreamingResolutionEnum
	GetLiveTranscodeInputArgs() []string
	GetLiveTranscodeOutputArgs() []string
	GetTranscodeHardwareAcceleration() bool
}

func NewStreamManager(cacheDir string, encoder *FFMpeg, ffprobe *FFProbe, config StreamManagerConfig, lockManager *fsutil.ReadLockManager) *StreamManager {
	if cacheDir == "" {
		logger.Warn("cache directory is not set. Live HLS/DASH transcoding will be disabled")
	}

	ctx, cancel := context.WithCancel(context.Background())

	ret := &StreamManager{
		cacheDir:         cacheDir,
		encoder:          encoder,
		ffprobe:          ffprobe,
		config:           config,
		lockManager:      lockManager,
		context:          ctx,
		cancelFunc:       cancel,
		runningStreams:   make(map[string]*runningStream),
		v3RunningStreams: make(map[string]*v3RunningStream),
	}

	go func() {
		for {
			select {
			case <-time.After(monitorInterval):
				ret.monitorStreams()
				ret.monitorV3Streams()
			case <-ctx.Done():
				return
			}
		}
	}()

	return ret
}

// Stop cancels media processes without removing state still used by HTTP handlers.
func (sm *StreamManager) Stop() {
	if sm.cancelFunc != nil {
		sm.cancelFunc()
	}
}

// Shutdown stops media processes and waits for cached stream cleanup.
func (sm *StreamManager) Shutdown() {
	_ = sm.ShutdownContext(context.Background())
}

// ShutdownContext runs cleanup once and bounds the wait, including when a
// stream lock or cache filesystem is stalled. HTTP handlers must drain first.
func (sm *StreamManager) ShutdownContext(ctx context.Context) error {
	sm.Stop()
	sm.shutdownOnce.Do(func() {
		sm.shutdownDone = make(chan struct{})
		go func() {
			defer close(sm.shutdownDone)
			sm.stopAndRemoveAll()
			sm.stopAndRemoveAllV3()
		}()
	})
	select {
	case <-sm.shutdownDone:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

type StreamRequestContext struct {
	context.Context
	ResponseWriter http.ResponseWriter
}

func NewStreamRequestContext(w http.ResponseWriter, r *http.Request) *StreamRequestContext {
	return &StreamRequestContext{
		Context:        r.Context(),
		ResponseWriter: w,
	}
}

func (c *StreamRequestContext) Cancel() {
	hj, ok := (c.ResponseWriter).(http.Hijacker)
	if !ok {
		return
	}

	// hijack and close the connection
	conn, bw, _ := hj.Hijack()
	if conn != nil {
		if bw != nil {
			// notify end of stream
			_, err := bw.WriteString("0\r\n")
			if err != nil {
				logger.Warnf("unable to write end of stream: %v", err)
			}
			_, err = bw.WriteString("\r\n")
			if err != nil {
				logger.Warnf("unable to write end of stream: %v", err)
			}

			// flush the buffer, but don't wait indefinitely
			timeout := make(chan struct{}, 1)
			go func() {
				_ = bw.Flush()
				close(timeout)
			}()

			const waitTime = time.Second

			select {
			case <-timeout:
			case <-time.After(waitTime):
				logger.Warnf("unable to flush buffer - closing connection")
			}
		}

		conn.Close()
	}
}
