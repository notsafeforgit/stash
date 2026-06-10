package models

type StudioTagHierarchyMode string

const (
	StudioTagHierarchyModeExact                StudioTagHierarchyMode = "exact"
	StudioTagHierarchyModeAncestors            StudioTagHierarchyMode = "ancestors"
	StudioTagHierarchyModeDescendants          StudioTagHierarchyMode = "descendants"
	StudioTagHierarchyModeAncestorsDescendants StudioTagHierarchyMode = "ancestors_descendants"
)

func (m StudioTagHierarchyMode) IsValid() bool {
	switch m {
	case "", StudioTagHierarchyModeExact, StudioTagHierarchyModeAncestors, StudioTagHierarchyModeDescendants, StudioTagHierarchyModeAncestorsDescendants:
		return true
	default:
		return false
	}
}

type StudioTagFilterInput struct {
	HierarchicalMultiCriterionInput
	HierarchyMode StudioTagHierarchyMode `json:"hierarchy_mode"`
}
