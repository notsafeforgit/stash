package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strconv"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/plugin/hook"
	"github.com/stashapp/stash/pkg/txn"
)

// FileHookExecutor dispatches notifications only for hooks with enabled listeners.
type FileHookExecutor interface {
	HasHooks(hook.TriggerEnum) bool
	ExecutePostHooks(context.Context, int, hook.TriggerEnum, interface{}, []string)
}

func (c Cache) HasHooks(trigger hook.TriggerEnum) bool {
	for _, p := range c.enabledPlugins() {
		if len(p.getHooks(trigger)) > 0 {
			return true
		}
	}
	return false
}

// FileDestroyInput preserves identity after the file record is gone. Destruction
// of the database entry does not necessarily delete the physical file.
type FileDestroyInput struct {
	ID           string            `json:"id"`
	Path         string            `json:"path"`
	Basename     string            `json:"basename"`
	ZipFileID    *string           `json:"zip_file_id"`
	Fingerprints map[string]string `json:"fingerprints"`
}

// FileUpdateInput contains snapshots taken within the committing transaction.
type FileUpdateInput struct {
	ID     string                 `json:"id"`
	Before map[string]interface{} `json:"before"`
	After  map[string]interface{} `json:"after"`
}

type fileHookRepository struct {
	models.FileReaderWriter
	hooks FileHookExecutor
}

// WithFileHooks observes file writes shared by API calls, scene/image/gallery
// deletion, scans and cleanup. Writes must run inside a txn.WithTxn transaction.
func WithFileHooks(files models.FileReaderWriter, hooks FileHookExecutor) models.FileReaderWriter {
	return &fileHookRepository{FileReaderWriter: files, hooks: hooks}
}

func (r *fileHookRepository) Destroy(ctx context.Context, id models.FileID) error {
	if !r.hooks.HasHooks(hook.FileDestroyPost) {
		return r.FileReaderWriter.Destroy(ctx, id)
	}

	files, err := r.Find(ctx, id)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return r.FileReaderWriter.Destroy(ctx, id)
	}
	b := files[0].Base()
	input := FileDestroyInput{
		ID: strconv.Itoa(int(id)), Path: b.Path, Basename: b.Basename,
		Fingerprints: make(map[string]string),
	}
	if b.ZipFileID != nil {
		zipID := strconv.Itoa(int(*b.ZipFileID))
		input.ZipFileID = &zipID
	}
	for _, fp := range b.Fingerprints {
		input.Fingerprints[fp.Type] = fp.Value()
	}
	if err := r.FileReaderWriter.Destroy(ctx, id); err != nil {
		return err
	}
	txn.AddPostCommitHook(ctx, func(ctx context.Context) {
		r.hooks.ExecutePostHooks(ctx, int(id), hook.FileDestroyPost, input, nil)
	})
	return nil
}

func (r *fileHookRepository) snapshot(ctx context.Context, id models.FileID) (map[string]interface{}, error) {
	files, err := r.Find(ctx, id)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("file with id %d not found", id)
	}
	f := files[0].Clone()
	f.Base().ZipFile = nil // Transient object, not a field of this file.
	if video, ok := f.(*models.VideoFile); ok {
		video.Duration = video.DurationFinite()
		video.FrameRate = video.FrameRateFinite()
		if video.VideoStreamDuration != nil {
			duration := video.VideoStreamDurationFinite()
			video.VideoStreamDuration = &duration
		}
	}
	data, err := json.Marshal(f)
	if err != nil {
		return nil, err
	}
	var ret map[string]interface{}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&ret); err != nil {
		return nil, err
	}
	// Preserve integer precision for raw/RPC plugins while exposing native
	// values (not json.RawMessage byte arrays) to embedded JavaScript plugins.
	for field, value := range ret {
		if number, ok := value.(json.Number); ok {
			if integer, err := number.Int64(); err == nil {
				ret[field] = integer
			} else {
				floating, err := number.Float64()
				if err != nil {
					return nil, err
				}
				ret[field] = floating
			}
		}
	}
	delete(ret, "ZipFile")
	fingerprints := make(map[string]string)
	for _, fp := range f.Base().Fingerprints {
		fingerprints[fp.Type] = fp.Value()
	}
	ret["fingerprints"] = fingerprints
	captions, err := r.GetCaptions(ctx, id)
	if err != nil {
		return nil, err
	}
	captionData, err := json.Marshal(captions)
	if err != nil {
		return nil, err
	}
	var captionSnapshot interface{}
	if err := json.Unmarshal(captionData, &captionSnapshot); err != nil {
		return nil, err
	}
	ret["captions"] = captionSnapshot
	return ret, nil
}

func (r *fileHookRepository) update(ctx context.Context, id models.FileID, write func() error) error {
	if !r.hooks.HasHooks(hook.FileUpdatePost) {
		return write()
	}
	before, err := r.snapshot(ctx, id)
	if err != nil {
		return err
	}
	if err := write(); err != nil {
		return err
	}
	after, err := r.snapshot(ctx, id)
	if err != nil {
		return err
	}
	var fields []string
	for field, value := range after {
		previous, existed := before[field]
		if !existed || !reflect.DeepEqual(previous, value) {
			fields = append(fields, field)
		}
	}
	for field := range before {
		if _, ok := after[field]; !ok {
			fields = append(fields, field)
		}
	}
	if len(fields) == 0 {
		return nil
	}
	sort.Strings(fields)
	input := FileUpdateInput{ID: strconv.Itoa(int(id)), Before: before, After: after}
	txn.AddPostCommitHook(ctx, func(ctx context.Context) {
		r.hooks.ExecutePostHooks(ctx, int(id), hook.FileUpdatePost, input, fields)
	})
	return nil
}

func (r *fileHookRepository) Update(ctx context.Context, f models.File) error {
	return r.update(ctx, f.Base().ID, func() error { return r.FileReaderWriter.Update(ctx, f) })
}

func (r *fileHookRepository) ModifyFingerprints(ctx context.Context, id models.FileID, fingerprints []models.Fingerprint) error {
	return r.update(ctx, id, func() error { return r.FileReaderWriter.ModifyFingerprints(ctx, id, fingerprints) })
}

func (r *fileHookRepository) DestroyFingerprints(ctx context.Context, id models.FileID, types []string) error {
	return r.update(ctx, id, func() error { return r.FileReaderWriter.DestroyFingerprints(ctx, id, types) })
}

func (r *fileHookRepository) UpdateCaptions(ctx context.Context, id models.FileID, captions []*models.VideoCaption) error {
	return r.update(ctx, id, func() error { return r.FileReaderWriter.UpdateCaptions(ctx, id, captions) })
}
