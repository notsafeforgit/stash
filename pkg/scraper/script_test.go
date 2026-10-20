package scraper

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScraperScriptChild(t *testing.T) {
	mode := os.Getenv("STASH_SCRAPER_SCRIPT_TEST")
	if mode == "" {
		t.Skip("subprocess fixture")
	}
	switch mode {
	case "echo":
		_, _ = io.Copy(os.Stdout, os.Stdin)
	case "invalid":
		fmt.Print("not JSON\n")
		time.Sleep(30 * time.Second)
	default:
		os.Exit(8)
	}
	os.Exit(0)
}

func TestScriptOutput(t *testing.T) {
	executable, err := os.Executable()
	require.NoError(t, err)
	command := []string{executable, "-test.run=^TestScraperScriptChild$"}
	s := scriptScraper{definition: Definition{Name: "test scraper", path: filepath.Join(t.TempDir(), "scraper.yml")}}
	for _, tc := range []struct {
		name  string
		input string
		ok    bool
	}{
		{"metadata", `{"name":"example"}`, true},
		{"extension fields", `{"name":"example","unknown_extension":true}`, true},
		{"trailing whitespace", "{\"name\":\"example\"}\n \t", true},
		{"wrong field type", `{"name":123}`, false},
		{"two results", `{"name":"example"} {}`, false},
		{"trailing garbage", `{"name":"example"} broken`, false},
		{"truncated JSON", `{"name":`, false},
		{"empty", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("STASH_SCRAPER_SCRIPT_TEST", "echo")
			var out models.ScrapedPerformer
			err := s.runScraperScript(context.Background(), command, tc.input, &out)
			if tc.ok {
				require.NoError(t, err)
				require.Equal(t, "example", *out.Name)
			} else {
				require.ErrorIs(t, err, ErrScraperScript)
			}
		})
	}
	t.Run("stop on invalid output", func(t *testing.T) {
		t.Setenv("STASH_SCRAPER_SCRIPT_TEST", "invalid")
		var out any
		start := time.Now()
		err := s.runScraperScript(context.Background(), command, "", &out)
		require.ErrorIs(t, err, ErrScraperScript)
		require.Less(t, time.Since(start), 4*time.Second)
	})
	t.Run("empty command", func(t *testing.T) {
		require.ErrorIs(t, s.runScraperScript(context.Background(), nil, "", nil), ErrScraperScript)
	})
	t.Run("caller timeout", func(t *testing.T) {
		t.Setenv("STASH_SCRAPER_SCRIPT_TEST", "echo")
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		var out any
		err := s.runScraperScript(ctx, command, strings.Repeat(" ", 4096), &out)
		require.ErrorIs(t, err, context.Canceled)
	})
}

func Test_imageInputFromImage_worksWithMultipleFiles(t *testing.T) {

	date, _ := models.ParseDate("2020-01-01")
	model := models.Image{
		ID:           1,
		Title:        "Test Image",
		URLs:         models.NewRelatedStrings([]string{"https://example.com/image.png"}),
		Date:         &date,
		Code:         "Code",
		Photographer: "Photographer",
		Files: models.NewRelatedFiles([]models.File{
			makeImageFile(1),
			makeImageFile(2),
		}),
	}

	input := imageInputFromImage(&model)

	assert.Equal(t, "1", input.ID)
	assert.Equal(t, "Test Image", input.Title)
	assert.Equal(t, "https://example.com/image.png", input.Urls[0])
	assert.Equal(t, "2020-01-01", *input.Date)
	assert.Equal(t, "Code", input.Code)
	assert.Equal(t, "Photographer", input.Photographer)
	assert.Equal(t, "/data/images/image_0001_.png", input.Files[0].Path)
	assert.Equal(t, "/data/images/image_0002_.png", input.Files[1].Path)
}

func getImageStringValue(index int, field string) string {
	return fmt.Sprintf("image_%04d_%s", index, field)
}

func makeImageFile(i int) *models.ImageFile {
	return &models.ImageFile{
		BaseFile: &models.BaseFile{
			Path:     "/data/images/" + getImageStringValue(i, ".png"),
			Basename: getImageStringValue(i, ".png"),
		},
		Height: 200,
		Width:  300,
	}
}
