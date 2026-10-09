package models

import "context"

// SourceThreadFacts are source-qualified relationships, not post equivalence or
// performer attribution. Missing parents need not be downloaded or fabricated.
type SourceThreadFacts struct {
	Namespace      string `json:"namespace" db:"namespace"`
	PostID         string `json:"post_id" db:"post_id"`
	ConversationID string `json:"conversation_id" db:"conversation_id"`
	ReplyID        string `json:"reply_id" db:"reply_id"`
	AuthorID       string `json:"author_id" db:"author_id"`
	ReplyAuthorID  string `json:"reply_author_id" db:"reply_author_id"`
}

type SourceThreadLink struct {
	SourceID string             `json:"source_id"`
	URL      string             `json:"url"`
	Post     *SourcePostSummary `json:"post"`
}

type SourceThreadView struct {
	PostUUID string             `json:"post_uuid"`
	Facts    *SourceThreadFacts `json:"facts"`
	Conflict bool               `json:"conflict"`
	Root     *SourceThreadLink  `json:"root"`
	Parent   *SourceThreadLink  `json:"parent"`
	Posts    []SourceThreadLink `json:"posts"`
	Next     string             `json:"next,omitempty"`
}

type SourceThreadReaderWriter interface {
	ObserveCapture(context.Context, *SourceCapture) (string, error)
	Read(context.Context, string, string, int) (*SourceThreadView, error)
}
