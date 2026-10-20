package api

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/stashapp/stash/internal/manager"
	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/internal/manager/task"
	"github.com/stashapp/stash/pkg/ffmpeg"
	"github.com/stashapp/stash/pkg/fsutil"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/utils"
)

var ErrOverriddenConfig = errors.New("cannot set overridden value")

func (r *mutationResolver) Setup(ctx context.Context, input manager.SetupInput) (bool, error) {
	err := manager.GetInstance().Setup(ctx, input)
	return err == nil, err
}

func (r *mutationResolver) DownloadFFMpeg(ctx context.Context) (string, error) {
	mgr := manager.GetInstance()
	configDir := mgr.Config.GetConfigPathAbs()

	// don't run if ffmpeg is already installed
	ffmpegPath := ffmpeg.FindFFMpeg(configDir)
	ffprobePath := ffmpeg.FindFFProbe(configDir)
	if ffmpegPath != "" && ffprobePath != "" {
		return "", fmt.Errorf("ffmpeg and ffprobe already installed at %s and %s", ffmpegPath, ffprobePath)
	}

	t := &task.DownloadFFmpegJob{
		ConfigDirectory: configDir,
		OnComplete: func(ctx context.Context) error {
			return r.settings.update(ctx, func(c *config.Config) (func(), error) {
				c.SetString(config.FFMpegPath, "")
				c.SetString(config.FFProbePath, "")
				return func() {
					r.settings.runtime.RefreshFFMpeg(ctx)
					r.settings.runtime.RefreshStreamManager()
				}, nil
			})
		},
	}

	jobID, err := mgr.JobManager.AddTransient(ctx, "Downloading ffmpeg...", t)

	return jobID, err
}

func setConfigString(c *config.Config, key string, value *string) {
	if value != nil {
		c.SetString(key, *value)
	}
}

func setConfigBool(c *config.Config, key string, value *bool) {
	if value != nil {
		c.SetBool(key, *value)
	}
}

func setConfigInt(c *config.Config, key string, value *int) {
	if value != nil {
		c.SetInt(key, *value)
	}
}

func setConfigFloat(c *config.Config, key string, value *float64) {
	if value != nil {
		c.SetFloat(key, *value)
	}
}

func (r *mutationResolver) ConfigureGeneral(ctx context.Context, input ConfigGeneralInput) (*ConfigGeneralResult, error) {
	err := r.settings.update(ctx, func(c *config.Config) (func(), error) {
		checkConfigOverride := func(key string) error {
			if c.HasOverride(key) {
				return fmt.Errorf("%w: %s", ErrOverriddenConfig, key)
			}

			return nil
		}

		var directories []string
		validateDir := func(key string, value string, optional bool) error {
			if err := checkConfigOverride(key); err != nil {
				return err
			}

			if !optional || value != "" {
				directories = append(directories, value)
			}

			return nil
		}

		existingDBPath := c.GetDatabasePath()
		if input.SourceTrackingEnabled != nil {
			if err := checkConfigOverride(config.SourceTrackingEnabled); err != nil {
				return nil, err
			}
			c.SetBool(config.SourceTrackingEnabled, *input.SourceTrackingEnabled)
		}

		if input.DatabasePath != nil && existingDBPath != *input.DatabasePath {
			if err := checkConfigOverride(config.Database); err != nil {
				return nil, err
			}

			ext := filepath.Ext(*input.DatabasePath)
			if ext != ".db" && ext != ".sqlite" && ext != ".sqlite3" {
				return nil, fmt.Errorf("invalid database path, use extension db, sqlite, or sqlite3")
			}
			c.SetString(config.Database, *input.DatabasePath)
		}

		existingBackupDirectoryPath := c.GetBackupDirectoryPath()
		if input.BackupDirectoryPath != nil && existingBackupDirectoryPath != *input.BackupDirectoryPath {
			if err := validateDir(config.BackupDirectoryPath, *input.BackupDirectoryPath, true); err != nil {
				return nil, err
			}

			c.SetString(config.BackupDirectoryPath, *input.BackupDirectoryPath)
		}

		existingDeleteTrashPath := c.GetDeleteTrashPath()
		if input.DeleteTrashPath != nil && existingDeleteTrashPath != *input.DeleteTrashPath {
			if err := validateDir(config.DeleteTrashPath, *input.DeleteTrashPath, true); err != nil {
				return nil, err
			}

			c.SetString(config.DeleteTrashPath, *input.DeleteTrashPath)
		}

		existingGeneratedPath := c.GetGeneratedPath()
		if input.GeneratedPath != nil && existingGeneratedPath != *input.GeneratedPath {
			if err := validateDir(config.Generated, *input.GeneratedPath, false); err != nil {
				return nil, err
			}

			c.SetString(config.Generated, *input.GeneratedPath)
		}

		refreshScraperCache := false
		refreshScraperSource := false
		existingScrapersPath := c.GetScrapersPath()
		if input.ScrapersPath != nil && existingScrapersPath != *input.ScrapersPath {
			if err := validateDir(config.ScrapersPath, *input.ScrapersPath, false); err != nil {
				return nil, err
			}

			refreshScraperCache = true
			refreshScraperSource = true
			c.SetString(config.ScrapersPath, *input.ScrapersPath)
		}

		refreshPluginCache := false
		refreshPluginSource := false
		existingPluginsPath := c.GetPluginsPath()
		if input.PluginsPath != nil && existingPluginsPath != *input.PluginsPath {
			if err := validateDir(config.PluginsPath, *input.PluginsPath, false); err != nil {
				return nil, err
			}

			refreshPluginCache = true
			refreshPluginSource = true
			c.SetString(config.PluginsPath, *input.PluginsPath)
		}

		existingMetadataPath := c.GetMetadataPath()
		if input.MetadataPath != nil && existingMetadataPath != *input.MetadataPath {
			if err := validateDir(config.Metadata, *input.MetadataPath, true); err != nil {
				return nil, err
			}

			c.SetString(config.Metadata, *input.MetadataPath)
		}

		refreshStreamManager := false
		existingCachePath := c.GetCachePath()
		if input.CachePath != nil && existingCachePath != *input.CachePath {
			if err := validateDir(config.Cache, *input.CachePath, true); err != nil {
				return nil, err
			}

			c.SetString(config.Cache, *input.CachePath)
			refreshStreamManager = true
		}

		refreshBlobStorage := false
		existingBlobsPath := c.GetBlobsPath()
		if input.BlobsPath != nil && existingBlobsPath != *input.BlobsPath {
			if err := validateDir(config.BlobsPath, *input.BlobsPath, true); err != nil {
				return nil, err
			}

			c.SetString(config.BlobsPath, *input.BlobsPath)
			refreshBlobStorage = true
		}

		if input.BlobsStorage != nil && *input.BlobsStorage != c.GetBlobsStorage() {
			if *input.BlobsStorage == config.BlobStorageTypeFilesystem && c.GetBlobsPath() == "" {
				return nil, fmt.Errorf("blobs path must be set when using filesystem storage")
			}

			c.SetInterface(config.BlobsStorage, *input.BlobsStorage)

			refreshBlobStorage = true
		}

		refreshFfmpeg := false
		if input.FfmpegPath != nil && *input.FfmpegPath != c.GetFFMpegPath() {
			if *input.FfmpegPath != "" {
				if err := ffmpeg.ValidateFFMpeg(*input.FfmpegPath); err != nil {
					return nil, fmt.Errorf("invalid ffmpeg path: %w", err)
				}
			}

			c.SetString(config.FFMpegPath, *input.FfmpegPath)
			refreshFfmpeg = true
		}

		if input.FfprobePath != nil && *input.FfprobePath != c.GetFFProbePath() {
			if *input.FfprobePath != "" {
				if err := ffmpeg.ValidateFFProbe(*input.FfprobePath); err != nil {
					return nil, fmt.Errorf("invalid ffprobe path: %w", err)
				}
			}

			c.SetString(config.FFProbePath, *input.FfprobePath)
			refreshFfmpeg = true
		}

		if input.VideoFileNamingAlgorithm != nil && *input.VideoFileNamingAlgorithm != c.GetVideoFileNamingAlgorithm() {
			calculateMD5 := c.IsCalculateMD5()
			if input.CalculateMd5 != nil {
				calculateMD5 = *input.CalculateMd5
			}
			if !calculateMD5 && *input.VideoFileNamingAlgorithm == models.HashAlgorithmMd5 {
				return nil, errors.New("calculateMD5 must be true if using MD5")
			}

			// validate changing VideoFileNamingAlgorithm
			if err := r.withReadTxn(ctx, func(ctx context.Context) error {
				return manager.ValidateVideoFileNamingAlgorithm(ctx, r.repository.Scene, *input.VideoFileNamingAlgorithm)
			}); err != nil {
				return nil, err
			}

			c.SetInterface(config.VideoFileNamingAlgorithm, *input.VideoFileNamingAlgorithm)
		}

		setConfigBool(c, config.CalculateMD5, input.CalculateMd5)
		setConfigInt(c, config.ParallelTasks, input.ParallelTasks)
		setConfigBool(c, config.PreviewAudio, input.PreviewAudio)
		setConfigInt(c, config.PreviewSegments, input.PreviewSegments)
		setConfigFloat(c, config.PreviewSegmentDuration, input.PreviewSegmentDuration)
		// Validate both marker-preview durations before applying either: 0 disables
		// the ceiling, so a negative max is rejected rather than silently treated as
		// disabled; the default is a fallback length and must be positive.
		if input.MaxMarkerPreviewDuration != nil && *input.MaxMarkerPreviewDuration < 0 {
			return nil, errors.New("maxMarkerPreviewDuration must be 0 (disabled) or a positive value")
		}
		if input.DefaultMarkerPreviewDuration != nil && *input.DefaultMarkerPreviewDuration <= 0 {
			return nil, errors.New("defaultMarkerPreviewDuration must be a positive value")
		}
		setConfigInt(c, config.MaxMarkerPreviewDuration, input.MaxMarkerPreviewDuration)
		setConfigInt(c, config.DefaultMarkerPreviewDuration, input.DefaultMarkerPreviewDuration)
		setConfigString(c, config.PreviewExcludeStart, input.PreviewExcludeStart)
		setConfigString(c, config.PreviewExcludeEnd, input.PreviewExcludeEnd)
		if input.PreviewPreset != nil {
			c.SetString(config.PreviewPreset, input.PreviewPreset.String())
		}
		setConfigBool(c, config.UseCustomSpriteInterval, input.UseCustomSpriteInterval)
		setConfigFloat(c, config.SpriteInterval, input.SpriteInterval)
		setConfigInt(c, config.MinimumSprites, input.MinimumSprites)
		setConfigInt(c, config.MaximumSprites, input.MaximumSprites)
		setConfigInt(c, config.SpriteScreenshotSize, input.SpriteScreenshotSize)

		setConfigBool(c, config.TranscodeHardwareAcceleration, input.TranscodeHardwareAcceleration)
		if input.MaxTranscodeSize != nil {
			c.SetString(config.MaxTranscodeSize, input.MaxTranscodeSize.String())
		}

		if input.MaxStreamingTranscodeSize != nil {
			c.SetString(config.MaxStreamingTranscodeSize, input.MaxStreamingTranscodeSize.String())
		}
		setConfigBool(c, config.WriteImageThumbnails, input.WriteImageThumbnails)
		setConfigBool(c, config.CreateImageClipsFromVideos, input.CreateImageClipsFromVideos)
		setConfigBool(c, config.BulkUpdateHooks, input.BulkUpdateHooks)

		if input.GalleryCoverRegex != nil {
			_, err := regexp.Compile(*input.GalleryCoverRegex)
			if err != nil {
				return nil, fmt.Errorf("Gallery cover regex '%v' invalid, '%v'", *input.GalleryCoverRegex, err.Error())
			}

			c.SetString(config.GalleryCoverRegex, *input.GalleryCoverRegex)
		}

		setConfigString(c, config.Username, input.Username)
		if input.ClearPassword != nil && *input.ClearPassword {
			if input.NewPassword != nil {
				return nil, errors.New("newPassword and clearPassword cannot be used together")
			}
			c.SetPassword("")
		} else if input.NewPassword != nil {
			if len(*input.NewPassword) == 0 || len(*input.NewPassword) > 72 {
				return nil, errors.New("newPassword must contain 1 to 72 UTF-8 bytes; use clearPassword to remove it")
			}
			c.SetPassword(*input.NewPassword)
		}

		setConfigInt(c, config.MaxSessionAge, input.MaxSessionAge)
		setConfigString(c, config.LogFile, input.LogFile)
		setConfigBool(c, config.LogOut, input.LogOut)
		setConfigBool(c, config.LogAccess, input.LogAccess)

		if input.LogLevel != nil && *input.LogLevel != c.GetLogLevel() {
			c.SetString(config.LogLevel, *input.LogLevel)
		}

		if input.LogFileMaxSize != nil && *input.LogFileMaxSize != c.GetLogFileMaxSize() {
			c.SetInt(config.LogFileMaxSize, *input.LogFileMaxSize)
		}

		if input.Excludes != nil {
			for _, r := range input.Excludes {
				_, err := regexp.Compile(r)
				if err != nil {
					return nil, fmt.Errorf("video exclusion pattern '%v' invalid: %w", r, err)
				}
			}
			c.SetInterface(config.Exclude, input.Excludes)
		}

		if input.ImageExcludes != nil {
			for _, r := range input.ImageExcludes {
				_, err := regexp.Compile(r)
				if err != nil {
					return nil, fmt.Errorf("image/gallery exclusion pattern '%v' invalid: %w", r, err)
				}
			}
			c.SetInterface(config.ImageExclude, input.ImageExcludes)
		}

		if input.VideoExtensions != nil {
			c.SetInterface(config.VideoExtensions, input.VideoExtensions)
		}

		if input.ImageExtensions != nil {
			c.SetInterface(config.ImageExtensions, input.ImageExtensions)
		}

		if input.GalleryExtensions != nil {
			c.SetInterface(config.GalleryExtensions, input.GalleryExtensions)
		}

		setConfigBool(c, config.CreateGalleriesFromFolders, input.CreateGalleriesFromFolders)

		if input.CustomPerformerImageLocation != nil {
			c.SetString(config.CustomPerformerImageLocation, *input.CustomPerformerImageLocation)
		}

		if input.StashBoxes != nil {
			if err := c.ValidateStashBoxes(input.StashBoxes); err != nil {
				return nil, err
			}
			c.SetInterface(config.StashBoxes, input.StashBoxes)
		}

		if input.PythonPath != nil {
			setConfigString(c, config.PythonPath, input.PythonPath)
		}

		if input.TranscodeInputArgs != nil {
			c.SetInterface(config.TranscodeInputArgs, input.TranscodeInputArgs)
		}
		if input.TranscodeOutputArgs != nil {
			c.SetInterface(config.TranscodeOutputArgs, input.TranscodeOutputArgs)
		}
		if input.LiveTranscodeInputArgs != nil {
			c.SetInterface(config.LiveTranscodeInputArgs, input.LiveTranscodeInputArgs)
		}
		if input.LiveTranscodeOutputArgs != nil {
			c.SetInterface(config.LiveTranscodeOutputArgs, input.LiveTranscodeOutputArgs)
		}

		setConfigBool(c, config.DrawFunscriptHeatmapRange, input.DrawFunscriptHeatmapRange)

		if input.ScraperPackageSources != nil {
			c.SetInterface(config.ScraperPackageSources, input.ScraperPackageSources)
			refreshScraperSource = true
		}

		if input.PluginPackageSources != nil {
			c.SetInterface(config.PluginPackageSources, input.PluginPackageSources)
			refreshPluginSource = true
		}

		// Directory preparation is idempotent. A failed save may leave an empty
		// directory, but never a partially published configuration.
		for _, directory := range directories {
			if err := fsutil.EnsureDir(directory); err != nil {
				return nil, err
			}
		}
		return func() {
			if input.LogLevel != nil {
				r.settings.setLogLevel(c.GetLogLevel())
			}
			if input.CustomPerformerImageLocation != nil {
				initCustomPerformerImages(c.GetCustomPerformerImageLocation())
			}

			r.settings.runtime.RefreshConfig()
			if refreshScraperCache {
				r.settings.runtime.RefreshScraperCache()
			}
			if refreshPluginCache {
				r.settings.runtime.RefreshPluginCache()
			}
			if refreshFfmpeg {
				r.settings.runtime.RefreshFFMpeg(ctx)

				// refresh stream manager is required since ffmpeg changed
				refreshStreamManager = true
			}
			if refreshStreamManager {
				r.settings.runtime.RefreshStreamManager()
			}
			if refreshBlobStorage {
				r.settings.runtime.SetBlobStoreOptions()
			}
			if refreshScraperSource {
				r.settings.runtime.RefreshScraperSourceManager()
			}
			if refreshPluginSource {
				r.settings.runtime.RefreshPluginSourceManager()
			}

		}, nil
	})
	if err != nil {
		return nil, err
	}
	return makeConfigGeneralResult(r.settings.config.Snapshot()), nil
}

func (r *mutationResolver) ConfigureInterface(ctx context.Context, input ConfigInterfaceInput) (*ConfigInterfaceResult, error) {
	err := r.settings.update(ctx, func(c *config.Config) (func(), error) {
		setConfigBool(c, config.SFWContentMode, input.SfwContentMode)

		if input.MenuItems != nil {
			c.SetInterface(config.MenuItems, input.MenuItems)
		}

		setConfigBool(c, config.SoundOnPreview, input.SoundOnPreview)
		setConfigBool(c, config.WallShowTitle, input.WallShowTitle)

		setConfigBool(c, config.NoBrowser, input.NoBrowser)

		setConfigBool(c, config.NotificationsEnabled, input.NotificationsEnabled)

		setConfigBool(c, config.ShowScrubber, input.ShowScrubber)

		setConfigString(c, config.WallPlayback, input.WallPlayback)
		if input.PreviewDefault != nil {
			c.SetString(config.PreviewDefault, strings.ToLower(input.PreviewDefault.String()))
		}
		setConfigBool(c, config.PlayVideoOnHover, input.PlayVideoOnHover)
		setConfigInt(c, config.MaximumLoopDuration, input.MaximumLoopDuration)
		setConfigBool(c, config.AutostartVideo, input.AutostartVideo)
		setConfigBool(c, config.ShowStudioAsText, input.ShowStudioAsText)
		setConfigBool(c, config.AutostartVideoOnPlaySelected, input.AutostartVideoOnPlaySelected)
		setConfigBool(c, config.ContinuePlaylistDefault, input.ContinuePlaylistDefault)

		setConfigString(c, config.Language, input.Language)

		if input.ImageLightbox != nil {
			options := input.ImageLightbox

			setConfigInt(c, config.ImageLightboxSlideshowDelay, options.SlideshowDelay)

			setConfigString(c, config.ImageLightboxDisplayModeKey, (*string)(options.DisplayMode))
			setConfigBool(c, config.ImageLightboxScaleUp, options.ScaleUp)
			setConfigBool(c, config.ImageLightboxResetZoomOnNav, options.ResetZoomOnNav)
			setConfigString(c, config.ImageLightboxScrollModeKey, (*string)(options.ScrollMode))

			setConfigInt(c, config.ImageLightboxScrollAttemptsBeforeChange, options.ScrollAttemptsBeforeChange)

			setConfigBool(c, config.ImageLightboxDisableAnimation, options.DisableAnimation)
		}

		if input.CSS != nil {
			c.SetCSS(*input.CSS)
		}

		setConfigBool(c, config.CSSEnabled, input.CSSEnabled)

		if input.Javascript != nil {
			c.SetJavascript(*input.Javascript)
		}

		setConfigBool(c, config.JavascriptEnabled, input.JavascriptEnabled)

		if input.CustomLocales != nil {
			c.SetCustomLocales(*input.CustomLocales)
		}

		setConfigBool(c, config.CustomLocalesEnabled, input.CustomLocalesEnabled)

		setConfigBool(c, config.DisableCustomizations, input.DisableCustomizations)

		if input.DisableDropdownCreate != nil {
			ddc := input.DisableDropdownCreate
			setConfigBool(c, config.DisableDropdownCreatePerformer, ddc.Performer)
			setConfigBool(c, config.DisableDropdownCreateStudio, ddc.Studio)
			setConfigBool(c, config.DisableDropdownCreateTag, ddc.Tag)
			setConfigBool(c, config.DisableDropdownCreateMovie, ddc.Movie)
			setConfigBool(c, config.DisableDropdownCreateGallery, ddc.Gallery)
		}

		setConfigString(c, config.HandyKey, input.HandyKey)
		setConfigInt(c, config.FunscriptOffset, input.FunscriptOffset)
		setConfigBool(c, config.UseStashHostedFunscript, input.UseStashHostedFunscript)

		return nil, nil
	})
	if err != nil {
		return nil, err
	}
	return makeConfigInterfaceResult(r.settings.config.Snapshot()), nil
}

func (r *mutationResolver) ConfigureDlna(ctx context.Context, input ConfigDLNAInput) (*ConfigDLNAResult, error) {
	err := r.settings.update(ctx, func(c *config.Config) (func(), error) {
		setConfigString(c, config.DLNAServerName, input.ServerName)

		if input.WhitelistedIPs != nil {
			c.SetInterface(config.DLNADefaultIPWhitelist, input.WhitelistedIPs)
		}

		setConfigString(c, config.DLNAVideoSortOrder, input.VideoSortOrder)
		setConfigInt(c, config.DLNAPort, input.Port)

		refresh := false
		if input.Enabled != nil {
			c.SetBool(config.DLNADefaultEnabled, *input.Enabled)
			refresh = true
		}

		if input.Interfaces != nil {
			c.SetInterface(config.DLNAInterfaces, input.Interfaces)
		}

		return func() {
			if refresh {
				r.settings.runtime.RefreshDLNA()
			}

		}, nil
	})
	if err != nil {
		return nil, err
	}
	return makeConfigDLNAResult(r.settings.config.Snapshot()), nil
}

func (r *mutationResolver) ConfigureScraping(ctx context.Context, input ConfigScrapingInput) (*ConfigScrapingResult, error) {
	err := r.settings.update(ctx, func(c *config.Config) (func(), error) {
		refreshScraperCache := false
		if input.ScraperUserAgent != nil {
			c.SetString(config.ScraperUserAgent, *input.ScraperUserAgent)
			refreshScraperCache = true
		}

		if input.ScraperCDPPath != nil {
			c.SetString(config.ScraperCDPPath, *input.ScraperCDPPath)
			refreshScraperCache = true
		}

		if input.ExcludeTagPatterns != nil {
			for _, r := range input.ExcludeTagPatterns {
				_, err := regexp.Compile(r)
				if err != nil {
					return nil, fmt.Errorf("tag exclusion pattern '%v' invalid: %w", r, err)
				}
			}
			c.SetInterface(config.ScraperExcludeTagPatterns, input.ExcludeTagPatterns)
		}

		setConfigBool(c, config.ScraperCertCheck, input.ScraperCertCheck)

		return func() {
			if refreshScraperCache {
				r.settings.runtime.RefreshScraperCache()
			}

		}, nil
	})
	if err != nil {
		return nil, err
	}
	return makeConfigScrapingResult(r.settings.config.Snapshot()), nil
}

func (r *mutationResolver) ConfigureDefaults(ctx context.Context, input ConfigDefaultSettingsInput) (*ConfigDefaultSettingsResult, error) {
	err := r.settings.update(ctx, func(c *config.Config) (func(), error) {
		if input.Identify != nil {
			c.SetInterface(config.DefaultIdentifySettings, input.Identify)
		}

		if input.Scan != nil {
			// if input.Scan is used then ScanMetadataOptions is included in the config file
			// this causes the values to not be read correctly
			c.SetInterface(config.DefaultScanSettings, input.Scan.ScanMetadataOptions)
		}

		if input.AutoTag != nil {
			c.SetInterface(config.DefaultAutoTagSettings, input.AutoTag)
		}

		if input.Generate != nil {
			c.SetInterface(config.DefaultGenerateSettings, input.Generate)
		}

		setConfigBool(c, config.DeleteFileDefault, input.DeleteFile)
		setConfigBool(c, config.DeleteGeneratedDefault, input.DeleteGenerated)

		return nil, manager.ValidateLibraryConfigReferences(ctx, c)
	})
	if err != nil {
		return nil, err
	}
	return makeConfigDefaultsResult(r.settings.config.Snapshot()), nil
}

func (r *mutationResolver) GenerateAPIKey(ctx context.Context, input GenerateAPIKeyInput) (string, error) {
	var newAPIKey string
	err := r.settings.update(ctx, func(c *config.Config) (func(), error) {
		newAPIKey = ""
		if (input.Clear == nil || !*input.Clear) && c.GetUsername() != "" {
			var err error
			newAPIKey, err = manager.GenerateAPIKey(c.GetUsername(), c.GetJWTSignKey())
			if err != nil {
				return nil, err
			}
		}
		c.SetString(config.ApiKey, newAPIKey)
		return nil, nil
	})
	if err != nil {
		return "", err
	}
	return newAPIKey, nil
}

func (r *mutationResolver) ConfigureUI(ctx context.Context, input map[string]interface{}, partial map[string]interface{}) (map[string]interface{}, error) {
	input = convertMapJSONNumbers(input)
	partial = convertMapJSONNumbers(partial)
	for _, values := range []map[string]interface{}{input, partial} {
		for key := range values {
			if nativeDefaultFilterConfigKey(key) {
				return nil, errors.New("default filters are native records; use configureDefaultFilter")
			}
		}
	}
	_, err := r.settings.config.UpdateUIConfiguration(func(existing map[string]interface{}) (map[string]interface{}, error) {
		if input != nil {
			existing = input
		}
		if partial != nil {
			utils.MergeMaps(existing, partial)
		}
		return existing, manager.ValidateLibraryUIReferences(ctx, existing)
	})
	if err != nil {
		return nil, err
	}
	return r.settings.runtime.UIConfiguration(ctx)
}

func (r *mutationResolver) ConfigureUISetting(ctx context.Context, key string, value interface{}) (map[string]interface{}, error) {
	if nativeDefaultFilterConfigKey(key) {
		return nil, errors.New("default filters are native records; use configureDefaultFilter")
	}
	value = convertJSONNumbers(value)
	_, err := r.settings.config.UpdateUIConfiguration(func(existing map[string]interface{}) (map[string]interface{}, error) {
		utils.NestedMap(existing).Set(key, value)
		return existing, manager.ValidateLibraryUIReferences(ctx, existing)
	})
	if err != nil {
		return nil, err
	}
	return r.settings.runtime.UIConfiguration(ctx)
}

func nativeDefaultFilterConfigKey(key string) bool {
	key = strings.SplitN(key, ".", 2)[0]
	return key == "defaultFilters" || key == "forkDefaultFilterState" || key == "defaultFilterConflicts"
}

func (r *mutationResolver) ConfigurePlugin(ctx context.Context, pluginID string, input map[string]interface{}) (map[string]interface{}, error) {
	input = convertMapJSONNumbers(input)
	if err := r.settings.update(ctx, func(c *config.Config) (func(), error) {
		c.SetPluginConfiguration(pluginID, input)
		return nil, nil
	}); err != nil {
		return nil, err
	}
	return r.settings.config.GetPluginConfiguration(pluginID), nil
}
