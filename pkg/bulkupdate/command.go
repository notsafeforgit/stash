package bulkupdate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/performer"
)

const MaxCommandBytes = 64 << 10

type Operation interface {
	Update(context.Context, int) error
}

// Command is the accepted edit, independent of HTTP inputs and live services.
// A semantic or incompatible payload change requires a new command version;
// pending supported versions must continue to decode or migrate explicitly.
type Command struct {
	Version int             `json:"version"`
	Kind    string          `json:"kind"`
	Payload json.RawMessage `json:"payload"`
}

type Services struct {
	Repository models.Repository
	Galleries  GalleryUpdater
	Groups     GroupUpdater
}

// Native partials retain Set/Null independently of Value. Date embeds time.Time,
// whose JSON method otherwise silently loses Precision, so the v1 wire format
// explicitly encodes the date fields. Keep this format stable across restarts.
type optionalDateV1 struct {
	Value struct {
		Time      time.Time
		Precision models.DatePrecision
	}
	Null bool
	Set  bool
}

func dateV1(d models.OptionalDate) optionalDateV1 {
	var ret optionalDateV1
	ret.Value.Time, ret.Value.Precision = d.Value.Time, d.Value.Precision
	ret.Null, ret.Set = d.Null, d.Set
	return ret
}

func (d optionalDateV1) native() models.OptionalDate {
	return models.OptionalDate{Value: models.Date{Time: d.Value.Time, Precision: d.Value.Precision}, Null: d.Null, Set: d.Set}
}

func (d *optionalDateV1) UnmarshalJSON(data []byte) error {
	type wire optionalDateV1
	var value wire
	if err := decodePayload(data, &value); err != nil {
		return err
	}
	if value.Value.Precision < models.DatePrecisionDay || value.Value.Precision > models.DatePrecisionYear {
		return errors.New("unsupported bulk command date precision")
	}
	*d = optionalDateV1(value)
	return nil
}

type sceneChangesV1 struct {
	models.ScenePartial
	Date           optionalDateV1
	ProductionDate optionalDateV1
}

type sceneV1 struct {
	Changes      sceneChangesV1
	CustomFields *models.CustomFieldsInput
}

type imageV1 struct {
	models.ImagePartial
	Date optionalDateV1
}

type galleryV1 struct {
	models.GalleryPartial
	Date optionalDateV1
}

type groupV1 struct {
	models.GroupPartial
	Date optionalDateV1
}

type performerChangesV1 struct {
	models.PerformerPartial
	Birthdate   optionalDateV1
	DeathDate   optionalDateV1
	CareerStart optionalDateV1
	CareerEnd   optionalDateV1
}

type performerV1 struct {
	Changes performerChangesV1
	URLs    performer.URLUpdate
}

func Encode(operation Operation) (Command, error) {
	if _, err := References(operation); err != nil {
		return Command{}, err
	}
	ret := Command{Version: 1}
	var payload any
	switch op := operation.(type) {
	case Scene:
		ret.Kind, payload = "scene.update", sceneV1{
			Changes: sceneChangesV1{op.Changes, dateV1(op.Changes.Date), dateV1(op.Changes.ProductionDate)}, CustomFields: op.CustomFields}
	case Image:
		ret.Kind, payload = "image.update", imageV1{op.Changes, dateV1(op.Changes.Date)}
	case Gallery:
		ret.Kind, payload = "gallery.update", galleryV1{op.Changes, dateV1(op.Changes.Date)}
	case Group:
		ret.Kind, payload = "group.update", groupV1{op.Changes, dateV1(op.Changes.Date)}
	case Performer:
		ret.Kind, payload = "performer.update", performerV1{
			Changes: performerChangesV1{op.Changes, dateV1(op.Changes.Birthdate), dateV1(op.Changes.DeathDate), dateV1(op.Changes.CareerStart), dateV1(op.Changes.CareerEnd)}, URLs: op.URLs}
	case Studio:
		ret.Kind, payload = "studio.update", op.Changes
	case Tag:
		ret.Kind, payload = "tag.update", op.Changes
	case SceneMarker:
		ret.Kind, payload = "scene_marker.update", op.Changes
	case SceneDateFromMTime:
		ret.Kind, payload = "scene.date_from_mtime", struct{}{}
	case ImageDateFromMTime:
		ret.Kind, payload = "image.date_from_mtime", struct{}{}
	default:
		return Command{}, errors.New("bulk operation has no supported durable command")
	}
	var err error
	ret.Payload, err = json.Marshal(payload)
	if err != nil {
		return Command{}, err
	}
	if len(ret.Payload) > MaxCommandBytes {
		return Command{}, errors.New("bulk command exceeds 64 KiB")
	}
	return ret, nil
}

func decodePayload(data []byte, value any) error {
	if len(data) > MaxCommandBytes {
		return errors.New("bulk command exceeds 64 KiB")
	}
	if trimmed := bytes.TrimSpace(data); len(trimmed) == 0 || trimmed[0] != '{' {
		return errors.New("bulk command payload must be an object")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	decoder.UseNumber()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return errors.New("bulk command has trailing data")
	}
	return nil
}

func Decode(command Command, services Services) (Operation, error) {
	op, err := decode(command, services)
	if err != nil {
		return nil, err
	}
	if _, err := References(op); err != nil {
		return nil, err
	}
	return op, nil
}

func decode(command Command, services Services) (Operation, error) {
	if command.Version != 1 {
		return nil, fmt.Errorf("unsupported bulk command version %d", command.Version)
	}
	r := services.Repository
	switch command.Kind {
	case "scene.update":
		var payload sceneV1
		if err := decodePayload(command.Payload, &payload); err != nil {
			return nil, err
		}
		p := payload.Changes.ScenePartial
		p.Date, p.ProductionDate = payload.Changes.Date.native(), payload.Changes.ProductionDate.native()
		return Scene{r.Scene, p, payload.CustomFields}, nil
	case "image.update":
		var payload imageV1
		if err := decodePayload(command.Payload, &payload); err != nil {
			return nil, err
		}
		p := payload.ImagePartial
		p.Date = payload.Date.native()
		return Image{r.Image, services.Galleries, p}, nil
	case "gallery.update":
		var payload galleryV1
		if err := decodePayload(command.Payload, &payload); err != nil {
			return nil, err
		}
		p := payload.GalleryPartial
		p.Date = payload.Date.native()
		return Gallery{r.Gallery, p}, nil
	case "group.update":
		var payload groupV1
		if err := decodePayload(command.Payload, &payload); err != nil {
			return nil, err
		}
		p := payload.GroupPartial
		p.Date = payload.Date.native()
		return Group{services.Groups, p}, nil
	case "performer.update":
		var payload performerV1
		if err := decodePayload(command.Payload, &payload); err != nil {
			return nil, err
		}
		p := payload.Changes.PerformerPartial
		p.Birthdate, p.DeathDate = payload.Changes.Birthdate.native(), payload.Changes.DeathDate.native()
		p.CareerStart, p.CareerEnd = payload.Changes.CareerStart.native(), payload.Changes.CareerEnd.native()
		return Performer{r.Performer, p, payload.URLs}, nil
	case "studio.update":
		var p models.StudioPartial
		err := decodePayload(command.Payload, &p)
		return Studio{r.Studio, p}, err
	case "tag.update":
		var p models.TagPartial
		err := decodePayload(command.Payload, &p)
		return Tag{r.Tag, p}, err
	case "scene_marker.update":
		var p models.SceneMarkerPartial
		err := decodePayload(command.Payload, &p)
		return SceneMarker{r.SceneMarker, p}, err
	case "scene.date_from_mtime":
		err := decodePayload(command.Payload, &struct{}{})
		return SceneDateFromMTime{r.Scene, r.File}, err
	case "image.date_from_mtime":
		err := decodePayload(command.Payload, &struct{}{})
		return ImageDateFromMTime{r.Image, r.File}, err
	default:
		return nil, fmt.Errorf("unsupported bulk command kind %q", command.Kind)
	}
}

func (c Command) EntityKind() (models.ArchiveEntityKind, error) {
	switch c.Kind {
	case "scene.update", "scene.date_from_mtime":
		return models.ArchiveScene, nil
	case "image.update", "image.date_from_mtime":
		return models.ArchiveImage, nil
	case "gallery.update":
		return models.ArchiveGallery, nil
	case "group.update":
		return models.ArchiveGroup, nil
	case "performer.update":
		return models.ArchivePerformer, nil
	case "studio.update":
		return models.ArchiveStudio, nil
	case "tag.update":
		return models.ArchiveTag, nil
	case "scene_marker.update":
		return models.ArchiveSceneMarker, nil
	default:
		return "", fmt.Errorf("unsupported bulk command kind %q", c.Kind)
	}
}
