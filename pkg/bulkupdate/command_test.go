package bulkupdate

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/performer"
	"github.com/stretchr/testify/require"
)

func TestCommandRoundTripPreservesAcceptedChanges(t *testing.T) {
	date := func(value string) models.OptionalDate {
		d, err := models.ParseDate(value)
		require.NoError(t, err)
		return models.NewOptionalDate(d)
	}
	updated := models.NewOptionalTime(time.Date(2026, 10, 20, 3, 4, 5, 6, time.UTC))
	custom := models.CustomFieldsInput{Full: map[string]any{}, Partial: map[string]any{"large": json.Number("9007199254740993")}, Remove: []string{"old"}}
	operations := []Operation{
		Scene{Changes: models.ScenePartial{Title: models.NewOptionalStringPtr(nil), Date: date("2020"),
			ProductionDate: date("2020-03"), UpdatedAt: updated, URLs: &models.UpdateStrings{Mode: models.RelationshipUpdateModeSet, Values: []string{}}},
			CustomFields: &custom},
		Image{Changes: models.ImagePartial{Title: models.NewOptionalString(""), Date: date("2020-03-04"), UpdatedAt: updated}},
		Gallery{Changes: models.GalleryPartial{Date: models.NewOptionalDatePtr(nil), UpdatedAt: updated}},
		Group{Changes: models.GroupPartial{Date: date("2022"), CustomFields: custom}},
		Performer{Changes: models.PerformerPartial{Birthdate: date("1990"), DeathDate: models.NewOptionalDatePtr(nil),
			CareerStart: date("2010-08"), CareerEnd: date("2020-01-02"), Country: models.NewOptionalStringPtr(nil),
			Favorite: models.NewOptionalBool(false), Rating: models.NewOptionalInt(0), UpdatedAt: updated, CustomFields: custom},
			URLs: performer.URLUpdate{Twitter: models.NewOptionalString("name")}},
		Studio{Changes: models.StudioPartial{Name: models.NewOptionalString("studio"), UpdatedAt: updated}},
		Tag{Changes: models.TagPartial{Description: models.NewOptionalStringPtr(nil), UpdatedAt: updated}},
		SceneMarker{Changes: models.SceneMarkerPartial{Title: models.NewOptionalString("marker"), UpdatedAt: updated}},
		SceneDateFromMTime{},
		ImageDateFromMTime{},
	}
	for _, operation := range operations {
		t.Run(reflect.TypeOf(operation).Name(), func(t *testing.T) {
			command, err := Encode(operation)
			require.NoError(t, err)
			resumed, err := Decode(command, Services{})
			require.NoError(t, err)
			require.Equal(t, operation, resumed, "omitted, null, empty and zero values must survive independently")
			reencoded, err := Encode(resumed)
			require.NoError(t, err)
			require.Equal(t, command, reencoded)
		})
	}
}

func TestCommandV1StoredFixturePreservesDatePrecisionAndClear(t *testing.T) {
	command := Command{Version: 1, Kind: "scene.update", Payload: json.RawMessage(`{
  "Changes": {
    "Title": {"Value": "", "Set": true, "Null": true},
    "Date": {"Value": {"Time": "2020-01-01T00:00:00Z", "Precision": 2}, "Set": true, "Null": false},
    "ProductionDate": {"Value": {"Time": "2020-03-01T00:00:00Z", "Precision": 1}, "Set": true, "Null": false},
    "TagIDs": {"ids": [], "mode": "SET"}
  },
  "CustomFields": {"full": {}, "partial": null, "remove": ["obsolete"]}
}`)}
	decoded, err := Decode(command, Services{})
	require.NoError(t, err)
	op := decoded.(Scene)
	require.True(t, op.Changes.Title.Set)
	require.True(t, op.Changes.Title.Null)
	require.False(t, op.Changes.Director.Set)
	require.Equal(t, "2020", op.Changes.Date.Value.String())
	require.Equal(t, "2020-03", op.Changes.ProductionDate.Value.String())
	require.NotNil(t, op.Changes.TagIDs)
	require.Empty(t, op.Changes.TagIDs.IDs)
	require.NotNil(t, op.CustomFields.Full)
	require.Nil(t, op.CustomFields.Partial)
}

func TestCommandCodecRejectsUnknownOrOversizedInputs(t *testing.T) {
	for _, tc := range []Command{
		{Version: 0, Kind: "scene.update", Payload: json.RawMessage("{}")},
		{Version: 2, Kind: "scene.update", Payload: json.RawMessage("{}")},
		{Version: 1, Kind: "shell.exec", Payload: json.RawMessage("{}")},
		{Version: 1, Kind: "scene.update", Payload: json.RawMessage("null")},
		{Version: 1, Kind: "scene.update", Payload: json.RawMessage("{} {}")},
		{Version: 1, Kind: "scene.update", Payload: json.RawMessage(`{"Changes":{"Unexpected":true}}`)},
		{Version: 1, Kind: "scene.update", Payload: json.RawMessage(`{"Changes":{"TagIDs":{"ids":[1],"mode":"UNKNOWN"}}}`)},
		{Version: 1, Kind: "scene.update", Payload: json.RawMessage(`{"Changes":{"TagIDs":{"ids":[0],"mode":"SET"}}}`)},
		{Version: 1, Kind: "image.update", Payload: json.RawMessage(`{"Date":{"Value":{"Time":"2020-01-01T00:00:00Z","Precision":3}}}`)},
		{Version: 1, Kind: "tag.update", Payload: json.RawMessage(strings.Repeat(" ", MaxCommandBytes+1))},
	} {
		_, err := Decode(tc, Services{})
		require.Error(t, err, "%s", tc.Payload)
	}
	_, err := Encode(Scene{Changes: models.ScenePartial{Details: models.NewOptionalString(strings.Repeat("x", MaxCommandBytes))}})
	require.ErrorContains(t, err, "64 KiB")
}

func TestCommandReferencesCaptureAllRelationshipKinds(t *testing.T) {
	list := func(ids ...int) *models.UpdateIDs {
		return &models.UpdateIDs{IDs: ids, Mode: models.RelationshipUpdateModeAdd}
	}
	fileID := models.FileID(9)
	for _, tc := range []struct {
		op   Operation
		want []Reference
	}{
		{Scene{Changes: models.ScenePartial{StudioID: models.NewOptionalInt(1), PrimaryFileID: &fileID, GalleryIDs: list(2),
			TagIDs: list(3, 3), PerformerIDs: list(4), GroupIDs: &models.UpdateGroupIDs{Mode: models.RelationshipUpdateModeSet, Groups: []models.GroupsScenes{{GroupID: 5}}}}},
			[]Reference{{models.ArchiveStudio, 1}, {models.ArchiveFile, 9}, {models.ArchiveGallery, 2}, {models.ArchiveTag, 3}, {models.ArchivePerformer, 4}, {models.ArchiveGroup, 5}}},
		{Image{Changes: models.ImagePartial{StudioID: models.NewOptionalInt(1), PrimaryFileID: &fileID, GalleryIDs: list(2), TagIDs: list(3), PerformerIDs: list(4)}},
			[]Reference{{models.ArchiveStudio, 1}, {models.ArchiveFile, 9}, {models.ArchiveGallery, 2}, {models.ArchiveTag, 3}, {models.ArchivePerformer, 4}}},
		{Gallery{Changes: models.GalleryPartial{StudioID: models.NewOptionalInt(1), PrimaryFileID: &fileID, SceneIDs: list(2), TagIDs: list(3), PerformerIDs: list(4)}},
			[]Reference{{models.ArchiveStudio, 1}, {models.ArchiveFile, 9}, {models.ArchiveScene, 2}, {models.ArchiveTag, 3}, {models.ArchivePerformer, 4}}},
		{Performer{Changes: models.PerformerPartial{TagIDs: list(3)}}, []Reference{{models.ArchiveTag, 3}}},
		{Studio{Changes: models.StudioPartial{ParentID: models.NewOptionalInt(1), TagIDs: list(3)}}, []Reference{{models.ArchiveStudio, 1}, {models.ArchiveTag, 3}}},
		{Tag{Changes: models.TagPartial{ParentIDs: list(1), ChildIDs: list(3)}}, []Reference{{models.ArchiveTag, 1}, {models.ArchiveTag, 3}}},
		{Group{Changes: models.GroupPartial{StudioID: models.NewOptionalInt(1), TagIDs: list(3),
			ContainingGroups: &models.UpdateGroupDescriptions{Mode: models.RelationshipUpdateModeSet, Groups: []models.GroupIDDescription{{GroupID: 2}}},
			SubGroups:        &models.UpdateGroupDescriptions{Mode: models.RelationshipUpdateModeSet, Groups: []models.GroupIDDescription{{GroupID: 4}}}}},
			[]Reference{{models.ArchiveStudio, 1}, {models.ArchiveTag, 3}, {models.ArchiveGroup, 2}, {models.ArchiveGroup, 4}}},
		{SceneMarker{Changes: models.SceneMarkerPartial{PrimaryTagID: models.NewOptionalInt(1), SceneID: models.NewOptionalInt(2), TagIDs: list(3)}},
			[]Reference{{models.ArchiveTag, 1}, {models.ArchiveScene, 2}, {models.ArchiveTag, 3}}},
	} {
		t.Run(reflect.TypeOf(tc.op).Name(), func(t *testing.T) {
			got, err := References(tc.op)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
	ids := make([]int, MaxReferences+1)
	for i := range ids {
		ids[i] = i + 1
	}
	_, err := References(Performer{Changes: models.PerformerPartial{TagIDs: list(ids...)}})
	require.ErrorContains(t, err, "1000")
}

// A newly added date field must receive an explicit precision-preserving wire
// field; embedding the native partial alone would silently inherit time.Time's
// lossy JSON method. This is a maintenance guard on future model changes.
func TestCommandDateFieldCoverage(t *testing.T) {
	for _, pair := range []struct{ native, wire any }{
		{models.ScenePartial{}, sceneChangesV1{}},
		{models.ImagePartial{}, imageV1{}},
		{models.GalleryPartial{}, galleryV1{}},
		{models.GroupPartial{}, groupV1{}},
		{models.PerformerPartial{}, performerChangesV1{}},
	} {
		native, wire := reflect.TypeOf(pair.native), reflect.TypeOf(pair.wire)
		for i := range native.NumField() {
			field := native.Field(i)
			if field.Type != reflect.TypeFor[models.OptionalDate]() {
				continue
			}
			encoded, ok := wire.FieldByName(field.Name)
			require.True(t, ok, "%s.%s has no wire field", native, field.Name)
			require.Equal(t, reflect.TypeFor[optionalDateV1](), encoded.Type, "%s.%s would lose precision", native, field.Name)
		}
	}
}
