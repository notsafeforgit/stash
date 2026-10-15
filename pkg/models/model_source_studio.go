package models

import "context"

// A source studio describes an aggregate publisher/collection, independently
// of the performers depicted in its media. Public references are always UUIDs.
type SourceStudio struct {
	UUID string `json:"uuid" db:"uuid"`
	Name string `json:"name" db:"name"`
}

type SourceStudioAssignment struct {
	CollectionUUID string        `json:"collection_uuid"`
	Revision       int           `json:"revision"`
	Studio         *SourceStudio `json:"studio"`
}

type SourceStudioInput struct {
	ExpectedRevision int     `json:"expected_revision"`
	StudioUUID       *string `json:"studio_uuid"`
}

type SourceStudioReaderWriter interface {
	Assignment(context.Context, string) (*SourceStudioAssignment, error)
	Assign(context.Context, string, SourceStudioInput) (*SourceStudioAssignment, error)
	ForPost(context.Context, string) ([]SourceStudio, error)
	ForGallery(context.Context, string) ([]SourceStudio, error)
}
