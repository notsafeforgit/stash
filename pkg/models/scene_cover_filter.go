package models

// DefaultSceneCoverFraction is the default frame position in the primary video.
const DefaultSceneCoverFraction = 0.2

// SceneCoverFrame describes the recorded frame relative to the current primary
// video. It does not infer whether untracked artwork was manually selected.
type SceneCoverFrame string

const (
	SceneCoverFrameDefault  SceneCoverFrame = "DEFAULT"
	SceneCoverFrameSpecific SceneCoverFrame = "SPECIFIC"
	SceneCoverFrameUnknown  SceneCoverFrame = "UNKNOWN"
)

func (v SceneCoverFrame) IsValid() bool {
	return v == SceneCoverFrameDefault || v == SceneCoverFrameSpecific || v == SceneCoverFrameUnknown
}

type SceneCoverFrameCriterionInput struct {
	Value    SceneCoverFrame   `json:"value"`
	Modifier CriterionModifier `json:"modifier"`
}
