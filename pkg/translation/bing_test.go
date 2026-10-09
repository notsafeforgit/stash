package translation

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

// The test binary acts as translate-shell without network access. It records
// literal argv so escaping, chunk boundaries and disabled init files are tested
// through the actual subprocess path.
func TestMain(m *testing.M) {
	if mode := os.Getenv("STASH_TEST_TRANSLATION_MODE"); mode != "" {
		if path := os.Getenv("STASH_TEST_TRANSLATION_ARGS"); path != "" {
			f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
			if err != nil {
				os.Exit(2)
			}
			err = json.NewEncoder(f).Encode(os.Args[1:])
			if closeErr := f.Close(); err != nil || closeErr != nil {
				os.Exit(2)
			}
		}
		switch mode {
		case "exit":
			fmt.Fprintln(os.Stderr, "private source text from provider")
			os.Exit(7)
		case "timeout":
			time.Sleep(30 * time.Second)
		case "overflow":
			fmt.Print(strings.Repeat("x", 2*1024*1024))
		case "malformed":
			fmt.Print(`[{"detectedLanguage":{"language":""},"translations":[]}]`)
		case "undetected":
			fmt.Print(`[{"translations":[{"text":"☀️🌦️","to":"en"}],"usedLLM":true}]`)
		case "partially_detected":
			if os.Args[len(os.Args)-1] == "☀️" {
				fmt.Print(`[{"translations":[{"text":"☀️","to":"en"}]}]`)
			} else {
				fmt.Print(`[{"detectedLanguage":{"language":"en"},"translations":[{"text":"First chunk","to":"en"}]}]`)
			}
		default:
			language := os.Getenv("STASH_TEST_TRANSLATION_LANGUAGE")
			if language == "" {
				language = "ja"
			}
			body, err := json.Marshal([]any{map[string]any{"detectedLanguage": map[string]string{"language": language}, "translations": []any{map[string]string{"to": "en", "text": "Translated chunk"}}}})
			if err != nil {
				os.Exit(2)
			}
			fmt.Print(string(body))
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func bingFixture(t *testing.T, mode string) (*BingTranslateShell, string) {
	t.Helper()
	if !providerPlatformSupported() {
		t.Skip("translate-shell requires a Unix host")
	}
	executable, err := os.Executable()
	require.NoError(t, err)
	provider, err := NewBingTranslateShell(executable)
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "arguments.jsonl")
	t.Setenv("STASH_TEST_TRANSLATION_MODE", mode)
	t.Setenv("STASH_TEST_TRANSLATION_ARGS", path)
	t.Setenv("GORACE", "atexit_sleep_ms=0")
	return provider, path
}

func bingRequest(text string) models.TranslationRequest {
	return models.TranslationRequest{TranslationRequestInput: models.TranslationRequestInput{OriginalText: text, TargetLanguage: "en", Policy: models.TranslationBingTextV1}}
}

func TestBingProviderLiteralUnicodeChunksAndURLProtection(t *testing.T) {
	provider, path := bingFixture(t, "translated")
	original := strings.Repeat("界", 1800) + "https://example.invalid/?literal=$(secret)`command`\n"
	result, err := provider.Translate(t.Context(), bingRequest(original))
	require.NoError(t, err)
	require.Equal(t, "translated", result.Status)
	require.Equal(t, "ja", *result.SourceLanguage)
	require.Equal(t, "Translated chunk\nTranslated chunk", *result.TranslatedText)
	body, err := os.ReadFile(path)
	require.NoError(t, err)
	rows := strings.Split(strings.TrimSpace(string(body)), "\n")
	require.Len(t, rows, 2)
	var first, second []string
	require.NoError(t, json.Unmarshal([]byte(rows[0]), &first))
	require.NoError(t, json.Unmarshal([]byte(rows[1]), &second))
	require.Equal(t, []string{"-no-init", "-engine", "bing", "-dump", "-no-ansi", ":en", "--"}, first[:7])
	require.Equal(t, strings.Repeat("界", 1800), first[7])
	require.Equal(t, " https://example.invalid/?literal=$(secret)`command`\n", second[7])
}

func TestBingProviderUnchangedKeepsExactOriginalAndNoTextSkipsProcess(t *testing.T) {
	provider, path := bingFixture(t, "translated")
	t.Setenv("STASH_TEST_TRANSLATION_LANGUAGE", "en-US")
	original := "  <p>Already &amp; unchanged</p>\n"
	result, err := provider.Translate(t.Context(), bingRequest(original))
	require.NoError(t, err)
	require.Equal(t, "unchanged", result.Status)
	require.Equal(t, original, *result.TranslatedText)
	body, err := os.ReadFile(path)
	require.NoError(t, err)
	var args []string
	require.NoError(t, json.Unmarshal(body, &args))
	require.Equal(t, "Already & unchanged", args[len(args)-1], "the provider receives the shared readable projection while the unchanged result retains the original")
	require.NoError(t, os.Remove(path))
	result, err = provider.Translate(t.Context(), bingRequest("<p> <br/> </p>"))
	require.NoError(t, err)
	require.Equal(t, "no_text", result.Status)
	require.Nil(t, result.TranslatedText)
	require.Nil(t, result.Provider)
	_, err = os.Stat(path)
	require.True(t, os.IsNotExist(err))
	_, err = provider.Translate(t.Context(), bingRequest("Cannot pass\x00in argv"))
	require.ErrorIs(t, err, ErrProviderInput)
	_, err = provider.Translate(t.Context(), bingRequest(strings.Repeat("x", 1800*128+1)))
	require.ErrorIs(t, err, ErrProviderInput, "oversized work is rejected, never truncated")
}

func TestBingProviderBoundsFailuresAndHonorsCancellation(t *testing.T) {
	for _, item := range []struct {
		mode     string
		expected error
	}{
		{"exit", ErrProviderFailure}, {"malformed", ErrProviderResponse}, {"overflow", ErrProviderResponse}, {"timeout", ErrProviderTimeout},
	} {
		t.Run(item.mode, func(t *testing.T) {
			provider, _ := bingFixture(t, item.mode)
			provider.ChunkTimeout = 150 * time.Millisecond
			started := time.Now()
			_, err := provider.Translate(t.Context(), bingRequest("Original source text"))
			require.ErrorIs(t, err, item.expected)
			require.NotContains(t, err.Error(), "private source text")
			require.Less(t, time.Since(started), 3*time.Second)
		})
	}
	provider, _ := bingFixture(t, "timeout")
	ctx, stop := context.WithTimeout(t.Context(), 150*time.Millisecond)
	defer stop()
	_, err := provider.Translate(ctx, bingRequest("Cancelled source text"))
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestBingProviderAllowsUndetectedLanguageWithoutInventingOne(t *testing.T) {
	provider, _ := bingFixture(t, "undetected")
	original := "<p>☀️🌦️</p>"
	result, err := provider.Translate(t.Context(), bingRequest(original))
	require.NoError(t, err)
	require.Equal(t, "translated", result.Status, "this is a provider result, not proof of an already-English source")
	require.Nil(t, result.SourceLanguage)
	require.Equal(t, "☀️🌦️", *result.TranslatedText)
	require.Equal(t, "translate-shell/bing", *result.Provider)
	request, err := archive.PrepareTranslationRequest(bingRequest(original).TranslationRequestInput)
	require.NoError(t, err)
	result.RequestUUID, result.Origin, result.CapturedAt = request.UUID, "worker", "2026-10-09T12:00:00Z"
	_, retained, err := archive.PrepareTranslationCache(request, result)
	require.NoError(t, err)
	require.Equal(t, original, *retained.OriginalText)
	require.Nil(t, retained.SourceLanguage)

	t.Setenv("STASH_TEST_TRANSLATION_MODE", "partially_detected")
	result, err = provider.Translate(t.Context(), bingRequest(strings.Repeat("a", 1800)+"☀️"))
	require.NoError(t, err)
	require.Nil(t, result.SourceLanguage, "one detected chunk cannot identify the entire source language")
	require.Equal(t, "translated", result.Status)
	require.Equal(t, "First chunk\n☀️", *result.TranslatedText)
}

func TestBingProviderOutputLimitCannotBeBypassedByCopy(t *testing.T) {
	stopped := false
	output := &providerOutput{stop: func() { stopped = true }}
	// Hide WriterTo so this exercises io.Copy's destination fast-path choice.
	reader := struct{ io.Reader }{strings.NewReader(strings.Repeat("x", 1024*1024+1))}
	_, err := io.Copy(output, reader)
	require.ErrorIs(t, err, ErrProviderResponse)
	require.True(t, stopped)
	require.True(t, output.overflow)
	require.LessOrEqual(t, output.buffer.Len(), 1024*1024)
}

func TestBingProviderRejectsCorruptUnicodeAndConflictingKeys(t *testing.T) {
	for _, raw := range []string{
		`[{"detectedLanguage":{"language":"ja"},"translations":[{"to":"en","text":"\ud800"}]}]`,
		`[{"detectedLanguage":{"language":"ja"},"translations":[{"to":"en","text":"first","text":"second"}]}]`,
		"[{\"detectedLanguage\":{\"language\":\"ja\"},\"translations\":[{\"to\":\"en\",\"text\":\"\xff\"}]}]",
	} {
		_, _, err := decodeBingResult([]byte(raw), "en")
		require.ErrorIs(t, err, ErrProviderResponse)
	}
}
