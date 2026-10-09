package translation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os/exec"
	"strings"
	"time"

	"github.com/stashapp/stash/pkg/archive"
	stashexec "github.com/stashapp/stash/pkg/exec"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/utils"
)

var (
	ErrProviderInput    = errors.New("translation provider cannot process this input")
	ErrProviderResponse = errors.New("translation provider returned invalid output")
	ErrProviderTimeout  = errors.New("translation provider timed out")
	ErrProviderFailure  = errors.New("translation provider failed")
)

// BingTranslateShell uses a locally configured executable, never a command
// supplied by a producer or API client. Configuration/init files are disabled.
type BingTranslateShell struct {
	Executable   string
	ChunkTimeout time.Duration
	Timeout      time.Duration
}

func NewBingTranslateShell(path string) (*BingTranslateShell, error) {
	if path == "" {
		path = "trans"
	}
	resolved, err := exec.LookPath(path)
	if err != nil {
		return nil, ErrProviderFailure
	}
	if !providerPlatformSupported() {
		return nil, ErrProviderInput
	}
	return &BingTranslateShell{Executable: resolved, ChunkTimeout: 30 * time.Second, Timeout: 5 * time.Minute}, nil
}

func languageBase(value string) string {
	return strings.Split(strings.ReplaceAll(strings.ToLower(strings.TrimSpace(value)), "_", "-"), "-")[0]
}

func (b *BingTranslateShell) Translate(ctx context.Context, request models.TranslationRequest) (models.TranslationCacheInput, error) {
	ret := models.TranslationCacheInput{}
	if _, err := archive.PrepareTranslationRequest(request.TranslationRequestInput); err != nil || b.ChunkTimeout <= 0 || b.Timeout <= 0 {
		return ret, ErrProviderInput
	}
	if err := ctx.Err(); err != nil {
		return ret, err
	}
	source := utils.ReadableText(request.OriginalText)
	if strings.TrimSpace(source) == "" {
		ret.Status = "no_text"
		return ret, nil
	}
	if strings.ContainsRune(source, 0) || b.Executable == "" {
		return ret, ErrProviderInput
	}
	chars := []rune(source)
	// Bounds total requests and memory without silently truncating long text.
	if len(chars) > 1800*128 {
		return ret, ErrProviderInput
	}
	bounded, stop := context.WithTimeout(ctx, b.Timeout)
	defer stop()
	outputs, languages := []string{}, map[string]bool{}
	unknownLanguage := false
	outputBytes := 0
	unchanged := true
	for offset := 0; offset < len(chars); offset += 1800 {
		chunk := string(chars[offset:min(offset+1800, len(chars))])
		if strings.HasPrefix(chunk, "http://") || strings.HasPrefix(chunk, "https://") || strings.HasPrefix(chunk, "file://") {
			chunk = " " + chunk
		}
		body, err := b.runChunk(bounded, request.TargetLanguage, chunk)
		if err != nil {
			if ctx.Err() != nil {
				return ret, ctx.Err()
			}
			return ret, err
		}
		language, output, err := decodeBingResult(body, request.TargetLanguage)
		if err != nil {
			return ret, err
		}
		if language == "" {
			unknownLanguage = true
		} else {
			languages[language] = true
		}
		outputBytes += len(output) + 1
		if outputBytes > archive.MaxTranslationTextBytes {
			return ret, ErrProviderResponse
		}
		unchanged = unchanged && languageBase(language) == languageBase(request.TargetLanguage)
		outputs = append(outputs, output)
	}
	language := ""
	if !unknownLanguage && len(languages) > 1 {
		language = "mul"
	} else if !unknownLanguage && len(languages) == 1 {
		for value := range languages {
			language = value
		}
	}
	output, status, provider := strings.Join(outputs, "\n"), "translated", "translate-shell/bing"
	if unchanged {
		// Distinct regional detections may still prove one target language.
		if len(languages) > 1 {
			language = languageBase(request.TargetLanguage)
		}
		output, status = request.OriginalText, "unchanged"
	}
	ret.Status, ret.TranslatedText, ret.Provider = status, &output, &provider
	if language != "" {
		ret.SourceLanguage = &language
	}
	return ret, nil
}

func decodeBingResult(raw []byte, target string) (string, string, error) {
	// Reject invalid Unicode and duplicate keys before json.Unmarshal can
	// replace bytes or silently choose one of two provider assertions.
	wrapped := append(append([]byte(`{"results":`), raw...), '}')
	if _, err := archive.DecodeJSONObject(wrapped, 1024*1024+12); err != nil {
		return "", "", ErrProviderResponse
	}
	var results []struct {
		DetectedLanguage struct {
			Language string `json:"language"`
		} `json:"detectedLanguage"`
		Translations []struct {
			To   string `json:"to"`
			Text string `json:"text"`
		} `json:"translations"`
	}
	// Bing can return a valid target-language result without detecting a source
	// language, for example for emoji. Preserve that uncertainty instead of
	// failing the job or claiming the source already uses the target language.
	if err := json.Unmarshal(raw, &results); err != nil || len(results) != 1 {
		return "", "", ErrProviderResponse
	}
	for _, output := range results[0].Translations {
		if output.To == target && strings.TrimSpace(output.Text) != "" {
			return results[0].DetectedLanguage.Language, output.Text, nil
		}
	}
	return "", "", ErrProviderResponse
}

type providerOutput struct {
	// Do not embed Buffer: its promoted ReadFrom would let io.Copy bypass
	// this writer's limit, including in os/exec's stdout-copy goroutine.
	buffer   bytes.Buffer
	stop     context.CancelFunc
	overflow bool
}

func (w *providerOutput) Write(body []byte) (int, error) {
	if w.buffer.Len()+len(body) > 1024*1024 {
		w.overflow = true
		w.stop()
		return 0, ErrProviderResponse
	}
	return w.buffer.Write(body)
}

func (b *BingTranslateShell) runChunk(ctx context.Context, target, payload string) ([]byte, error) {
	bounded, stop := context.WithTimeout(ctx, b.ChunkTimeout)
	defer stop()
	command := stashexec.CommandContext(bounded, b.Executable, "-no-init", "-engine", "bing", "-dump", "-no-ansi", ":"+target, "--", payload)
	if err := prepareProviderCommand(command); err != nil {
		return nil, err
	}
	output := &providerOutput{stop: stop}
	command.Stdout, command.Stderr, command.WaitDelay = output, io.Discard, time.Second
	err := command.Run()
	if output.overflow {
		return nil, ErrProviderResponse
	}
	if bounded.Err() != nil {
		return nil, ErrProviderTimeout
	}
	if err != nil {
		return nil, ErrProviderFailure
	}
	return output.buffer.Bytes(), nil
}
