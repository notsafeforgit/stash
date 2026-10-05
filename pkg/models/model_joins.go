package models

import "strconv"

type GroupsScenes struct {
	GroupID int `json:"movie_id"`
	// SceneID    int  `json:"scene_id"`
	SceneIndex *int `json:"scene_index"`
}

func (s GroupsScenes) SceneGroupInput() SceneGroupInput {
	return SceneGroupInput{
		GroupID:    strconv.Itoa(s.GroupID),
		SceneIndex: s.SceneIndex,
	}
}

func (s GroupsScenes) Equal(o GroupsScenes) bool {
	return o.GroupID == s.GroupID && ((o.SceneIndex == nil && s.SceneIndex == nil) ||
		(o.SceneIndex != nil && s.SceneIndex != nil && *o.SceneIndex == *s.SceneIndex))
}

type UpdateGroupIDs struct {
	Groups []GroupsScenes         `json:"movies"`
	Mode   RelationshipUpdateMode `json:"mode"`
}

func (u *UpdateGroupIDs) SceneGroupInputs() []SceneGroupInput {
	if u == nil {
		return nil
	}

	ret := make([]SceneGroupInput, 0, len(u.Groups))
	for _, id := range u.Groups {
		ret = append(ret, id.SceneGroupInput())
	}

	return ret
}

func (u *UpdateGroupIDs) AddUnique(v GroupsScenes) {
	for _, vv := range u.Groups {
		if vv.GroupID == v.GroupID {
			return
		}
	}

	u.Groups = append(u.Groups, v)
}

type GroupIDDescription struct {
	GroupID     int    `json:"group_id"`
	Description string `json:"description"`
}
