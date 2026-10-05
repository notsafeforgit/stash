package scene

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

type filenameGroupFinder struct{}

func (filenameGroupFinder) FindByName(_ context.Context, name string, _ bool) (*models.Group, error) {
	if name == "Example Group" {
		return &models.Group{ID: 42, Name: name}, nil
	}
	return nil, nil
}

func TestFilenameParserNativeGroups(t *testing.T) {
	for _, field := range []string{"group", "movie"} {
		t.Run(field, func(t *testing.T) {
			pattern := "{" + field + "}.mp4"
			p := NewFilenameParser(&models.FindFilterType{Q: &pattern}, models.SceneParserInput{}, FilenameParserRepository{})
			mapper, err := newParseMapper(pattern, nil)
			require.NoError(t, err)
			holder := mapper.parse(&models.Scene{Path: "/library/Example_Group.mp4"})
			require.NotNil(t, holder)
			holder.groups = append(holder.groups, "Example Group", "missing")
			result := &models.SceneParserResult{}
			p.setGroups(context.Background(), filenameGroupFinder{}, *holder, result)
			require.Equal(t, []*models.SceneGroupID{{GroupID: "42"}}, result.Groups)
			encoded, err := json.Marshal(result)
			require.NoError(t, err)
			require.Contains(t, string(encoded), `"groups":[{"group_id":"42"`)
			require.NotContains(t, string(encoded), `"movies"`)
		})
	}
}
