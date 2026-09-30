package api

import (
	"context"
	"strconv"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/plugin/hook"
)

// PerformerMergeProfile is an identity snapshot, including URLs and aliases
// that may no longer exist once the source performers have been deleted.
type PerformerMergeProfile struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Disambiguation string   `json:"disambiguation"`
	AliasList      []string `json:"alias_list"`
	URLs           []string `json:"urls"`
}

type PerformerMergeNotification struct {
	Destination         PerformerMergeProfile   `json:"destination"`
	PreviousDestination PerformerMergeProfile   `json:"previous_destination"`
	Sources             []PerformerMergeProfile `json:"sources"`
}

func (r *mutationResolver) capturePerformerMerge(ctx context.Context, destination *models.Performer, sources []*models.Performer) (*PerformerMergeNotification, error) {
	if r.hookExecutor == nil {
		return nil, nil
	}
	if listeners, ok := r.hookExecutor.(interface{ HasHooks(hook.TriggerEnum) bool }); ok && !listeners.HasHooks(hook.PerformerMergePost) {
		return nil, nil
	}
	ret := &PerformerMergeNotification{Sources: make([]PerformerMergeProfile, 0, len(sources))}
	var err error
	ret.PreviousDestination, err = r.performerMergeProfile(ctx, destination)
	if err != nil {
		return nil, err
	}
	for _, source := range sources {
		profile, err := r.performerMergeProfile(ctx, source)
		if err != nil {
			return nil, err
		}
		ret.Sources = append(ret.Sources, profile)
	}
	return ret, nil
}

func (r *mutationResolver) performerMergeProfile(ctx context.Context, p *models.Performer) (PerformerMergeProfile, error) {
	ret := PerformerMergeProfile{ID: strconv.Itoa(p.ID), Name: p.Name, Disambiguation: p.Disambiguation, AliasList: []string{}, URLs: []string{}}
	if err := p.LoadAliases(ctx, r.repository.Performer); err != nil {
		return ret, err
	}
	if err := p.LoadURLs(ctx, r.repository.Performer); err != nil {
		return ret, err
	}
	for _, alias := range p.Aliases.List() {
		ret.AliasList = append(ret.AliasList, alias.Alias)
	}
	ret.URLs = append(ret.URLs, p.URLs.List()...)
	return ret, nil
}
