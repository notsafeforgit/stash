package gallery

import (
	"context"
	"errors"
	"fmt"

	"github.com/stashapp/stash/pkg/file"
	"github.com/stashapp/stash/pkg/image"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/scene"
)

// Deletion handles an explicit library selection. Membership, rather than a
// media file's parent directory, determines which images and scenes it affects.
// The caller keeps planning and deletion in one repository transaction.
type Deletion struct {
	Repository models.Repository
	Images     interface {
		Destroy(context.Context, *models.Image, *image.FileDeleter, bool, bool, bool) error
	}
	Scenes interface {
		Destroy(context.Context, *models.Scene, *scene.FileDeleter, bool, bool, bool) error
	}
}

type DeletionOptions struct {
	Media, SharedMedia, Files, Generated, FileEntries bool
}

func (o DeletionOptions) Validate() error {
	if !o.Media && (o.SharedMedia || o.Files || o.FileEntries) {
		return errors.New("deleting media files, file entries or shared media requires delete_media")
	}
	return nil
}

type DeletionPlan struct {
	Galleries    []*models.Gallery
	Images       []*models.Image
	Scenes       []*models.Scene
	SharedImages map[int]bool
	SharedScenes map[int]bool
	ZipFiles     []models.File
	galleryIDs   map[int]bool
}

type DeletionResult struct {
	Images []*models.Image
	Scenes []*models.Scene
	// Only backing directories with no surviving gallery, never ZIP paths.
	EmptyFolders []string
}

func (d *Deletion) Plan(ctx context.Context, ids []int) (*DeletionPlan, error) {
	if len(ids) == 0 {
		return nil, errors.New("select at least one gallery")
	}
	p := &DeletionPlan{galleryIDs: map[int]bool{}, SharedImages: map[int]bool{}, SharedScenes: map[int]bool{}}
	images, scenes, zips := map[int]bool{}, map[int]bool{}, map[models.FileID]bool{}
	for _, id := range ids {
		if p.galleryIDs[id] {
			continue
		}
		g, err := d.Repository.Gallery.Find(ctx, id)
		if err != nil {
			return nil, err
		}
		if g == nil {
			return nil, fmt.Errorf("gallery with id %d not found", id)
		}
		p.galleryIDs[id] = true
		p.Galleries = append(p.Galleries, g)
		if err := g.LoadFiles(ctx, d.Repository.Gallery); err != nil {
			return nil, err
		}
		for _, f := range g.Files.List() {
			if !zips[f.Base().ID] {
				p.ZipFiles = append(p.ZipFiles, f)
				zips[f.Base().ID] = true
			}
		}
		members, err := d.Repository.Image.FindByGalleryID(ctx, id)
		if err != nil {
			return nil, err
		}
		for _, member := range members {
			if !images[member.ID] {
				p.Images = append(p.Images, member)
				images[member.ID] = true
			}
		}
		videos, err := d.Repository.Scene.FindByGalleryID(ctx, id)
		if err != nil {
			return nil, err
		}
		for _, member := range videos {
			if !scenes[member.ID] {
				p.Scenes = append(p.Scenes, member)
				scenes[member.ID] = true
			}
		}
	}
	for _, member := range p.Images {
		if err := member.LoadGalleryIDs(ctx, d.Repository.Image); err != nil {
			return nil, err
		}
		for _, id := range member.GalleryIDs.List() {
			if !p.galleryIDs[id] {
				p.SharedImages[member.ID] = true
			}
		}
	}
	for _, member := range p.Scenes {
		if err := member.LoadGalleryIDs(ctx, d.Repository.Scene); err != nil {
			return nil, err
		}
		for _, id := range member.GalleryIDs.List() {
			if !p.galleryIDs[id] {
				p.SharedScenes[member.ID] = true
			}
		}
	}
	return p, nil
}

// deletableZip checks the entire archive, not only the chosen gallery's images.
// Surviving owners and unassigned entries prevent a whole-archive deletion.
func (d *Deletion) deletableZip(ctx context.Context, p *DeletionPlan, f models.File, images, scenes map[int]bool) (bool, error) {
	owners, err := d.Repository.Gallery.FindByFileID(ctx, f.Base().ID)
	if err != nil {
		return false, err
	}
	for _, owner := range owners {
		if !p.galleryIDs[owner.ID] {
			return false, nil
		}
	}
	files, err := d.Repository.File.FindByZipFileID(ctx, f.Base().ID)
	if err != nil {
		return false, err
	}
	ids := make([]models.FileID, 0, len(files))
	for _, entry := range files {
		ids = append(ids, entry.Base().ID)
	}
	imageIDs, err := d.Repository.Image.GetManyIDsByFileIDs(ctx, ids)
	if err != nil {
		return false, err
	}
	sceneIDs, err := d.Repository.Scene.GetManyIDsByFileIDs(ctx, ids)
	if err != nil {
		return false, err
	}
	for i := range ids {
		if len(imageIDs[i])+len(sceneIDs[i]) == 0 {
			return false, nil
		}
		for _, id := range imageIDs[i] {
			if !images[id] {
				return false, nil
			}
		}
		for _, id := range sceneIDs[i] {
			if !scenes[id] {
				return false, nil
			}
		}
	}
	return true, nil
}

func (d *Deletion) Apply(ctx context.Context, p *DeletionPlan, options DeletionOptions, imageFiles *image.FileDeleter, sceneFiles *scene.FileDeleter, beforeScene func(*models.Scene)) (*DeletionResult, error) {
	if err := options.Validate(); err != nil {
		return nil, err
	}
	ret := &DeletionResult{}
	images, scenes := map[int]bool{}, map[int]bool{}
	if options.Media {
		for _, member := range p.Images {
			if options.SharedMedia || !p.SharedImages[member.ID] {
				ret.Images = append(ret.Images, member)
				images[member.ID] = true
			}
		}
		for _, member := range p.Scenes {
			if options.SharedMedia || !p.SharedScenes[member.ID] {
				ret.Scenes = append(ret.Scenes, member)
				scenes[member.ID] = true
			}
		}
	}
	var zips []models.File
	if options.Files || options.FileEntries {
		for _, f := range p.ZipFiles {
			allowed, err := d.deletableZip(ctx, p, f, images, scenes)
			if err != nil {
				return nil, err
			}
			if allowed {
				zips = append(zips, f)
			}
		}
	}
	for _, member := range ret.Images {
		if err := d.Images.Destroy(ctx, member, imageFiles, options.Generated, options.Files, options.FileEntries); err != nil {
			return nil, err
		}
	}
	for _, member := range ret.Scenes {
		if beforeScene != nil {
			beforeScene(member)
		}
		if err := d.Scenes.Destroy(ctx, member, sceneFiles, options.Generated, options.Files, options.FileEntries); err != nil {
			return nil, err
		}
	}
	for _, g := range p.Galleries {
		if err := d.Repository.Gallery.Destroy(ctx, g.ID); err != nil {
			return nil, err
		}
	}
	for _, f := range zips {
		destroyer := file.ZipDestroyer{FileDestroyer: d.Repository.File, FolderDestroyer: d.Repository.Folder}
		if err := destroyer.DestroyZip(ctx, f, imageFiles.Deleter, options.Files); err != nil {
			return nil, err
		}
	}
	if options.Files {
		for _, g := range p.Galleries {
			if g.FolderID == nil || g.Path == "" {
				continue
			}
			remaining, err := d.Repository.Gallery.FindByFolderID(ctx, *g.FolderID)
			if err != nil {
				return nil, err
			}
			if len(remaining) == 0 {
				ret.EmptyFolders = append(ret.EmptyFolders, g.Path)
			}
		}
	}
	return ret, nil
}
