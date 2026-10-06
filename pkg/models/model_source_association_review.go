package models

import (
	"errors"
	"time"
)

// Source association review changes selected links only. Gallery membership,
// metadata and file operations remain separate reviewed actions.
type GalleryAssociationReviewInput struct {
	PostUUID        string `json:"post_uuid"`
	PostRevision    int    `json:"post_revision"`
	State           string `json:"state"`
	GalleryUUID     string `json:"gallery_uuid,omitempty"`
	GalleryRevision int    `json:"gallery_revision,omitempty"`
	Reason          string `json:"reason,omitempty"`
}

type GalleryAssociationReviewPreview struct {
	Input    GalleryAssociationReviewInput `json:"input"`
	Current  *SourcePostAlbum              `json:"current"`
	Proposed *SourcePostLibraryItem        `json:"proposed"`
	Changed  bool                          `json:"changed"`
	Digest   string                        `json:"digest"`
}

type GalleryAssociationReviewApplyInput struct {
	GalleryAssociationReviewInput
	RequestUUID string `json:"request_uuid"`
	Digest      string `json:"digest"`
}

type GalleryAssociationReview struct {
	RequestUUID  string                             `json:"request_uuid"`
	DecisionUUID string                             `json:"decision_uuid"`
	Request      GalleryAssociationReviewApplyInput `json:"request"`
	CreatedAt    time.Time                          `json:"created_at"`
}

type AttachmentMediaReviewInput struct {
	PostUUID           string `json:"post_uuid"`
	PostRevision       int    `json:"post_revision"`
	AttachmentUUID     string `json:"attachment_uuid"`
	AttachmentRevision int    `json:"attachment_revision"`
	State              string `json:"state"`
	MediaUUID          string `json:"media_uuid,omitempty"`
	MediaRevision      int    `json:"media_revision,omitempty"`
	Reason             string `json:"reason,omitempty"`
}

type AttachmentMediaReviewDecision struct {
	UUID           string    `json:"uuid"`
	AttachmentUUID string    `json:"attachment_uuid"`
	Revision       int       `json:"revision"`
	State          string    `json:"state"`
	MediaUUID      *string   `json:"media_uuid"`
	Origin         string    `json:"origin"`
	Reason         string    `json:"reason"`
	CreatedAt      time.Time `json:"created_at"`
}

type AttachmentMediaReviewContext struct {
	PostUUID         string                         `json:"post_uuid"`
	PostRevision     int                            `json:"post_revision"`
	PostState        string                         `json:"post_state"`
	Attachment       SourceAlbumAttachment          `json:"attachment"`
	Current          *AttachmentMediaReviewDecision `json:"current"`
	Media            *SourcePostLibraryItem         `json:"media"`
	PostLinkState    string                         `json:"post_link_state"`
	SourceMediaKinds []string                       `json:"source_media_kinds"`
}

type AttachmentMediaReviewPreview struct {
	Input    AttachmentMediaReviewInput   `json:"input"`
	Current  AttachmentMediaReviewContext `json:"current"`
	Proposed *SourcePostLibraryItem       `json:"proposed"`
	Changed  bool                         `json:"changed"`
	Digest   string                       `json:"digest"`
}

type AttachmentMediaReviewApplyInput struct {
	AttachmentMediaReviewInput
	RequestUUID string `json:"request_uuid"`
	Digest      string `json:"digest"`
}

type AttachmentMediaReview struct {
	RequestUUID  string                          `json:"request_uuid"`
	DecisionUUID string                          `json:"decision_uuid"`
	Request      AttachmentMediaReviewApplyInput `json:"request"`
	CreatedAt    time.Time                       `json:"created_at"`
}

var (
	ErrSourceAssociationReviewInvalid  = errors.New("invalid source association review")
	ErrSourceAssociationReviewReplay   = errors.New("source association review UUID has different contents")
	ErrSourceAssociationReviewConflict = errors.New("source association preview changed")
)
