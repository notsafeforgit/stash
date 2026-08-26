package api

import (
	"context"
	"fmt"
	"strings"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/performer"
)

// preservePerformerMergeNames makes canonical-name retention a server-side
// merge invariant. Clients may still choose the final canonical name and alias
// set, but every merged performer's canonical name is retained as an alias
// unless it becomes the final canonical name itself.
//
// legacyAliasList identifies v2.5's deprecated alias_list input. That input has
// no per-alias policy field, so performerPartialFromInput assigns true as a
// compatibility default. For names that already exist, retain their stored
// policy instead of treating that synthetic value as an explicit policy edit.
func preservePerformerMergeNames(
	ctx context.Context,
	repository models.PerformerReaderWriter,
	destination *models.Performer,
	sources []*models.Performer,
	values *models.PerformerPartial,
	legacyAliasList bool,
) error {
	mergedPerformers := make([]*models.Performer, 0, len(sources)+1)
	mergedPerformers = append(mergedPerformers, destination)
	mergedPerformers = append(mergedPerformers, sources...)

	for _, p := range mergedPerformers {
		if err := p.LoadAliases(ctx, repository); err != nil {
			return fmt.Errorf("loading aliases for performer %d before merge: %w", p.ID, err)
		}
	}

	existingPolicies := make(map[string]bool)
	rememberPolicy := func(name string, ignoreAutoTag bool) {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			return
		}

		key := strings.ToLower(trimmed)
		existingPolicies[key] = existingPolicies[key] || ignoreAutoTag
	}

	for _, p := range mergedPerformers {
		rememberPolicy(p.Name, p.IgnorePrimaryNameAutoTag)
		for _, alias := range p.Aliases.List() {
			rememberPolicy(alias.Alias, alias.IgnoreAutoTag)
		}
	}

	aliases := append([]models.PerformerAlias(nil), destination.Aliases.List()...)
	if values.Aliases != nil {
		aliases = performer.GetEffectiveAliases(
			destination.Aliases.List(),
			values.Aliases.Values,
			values.Aliases.Mode,
			false,
		)
	}

	requestedPolicies := make(map[string]bool)
	for _, alias := range aliases {
		key := strings.ToLower(strings.TrimSpace(alias.Alias))
		requestedPolicies[key] = requestedPolicies[key] || alias.IgnoreAutoTag
	}

	policyForName := func(name string) (bool, bool) {
		key := strings.ToLower(strings.TrimSpace(name))
		existing, exists := existingPolicies[key]
		requested, requestedExists := requestedPolicies[key]

		if legacyAliasList && exists {
			return existing, true
		}
		if exists || requestedExists {
			return existing || requested, true
		}
		return false, false
	}

	for i := range aliases {
		if policy, ok := policyForName(aliases[i].Alias); ok {
			aliases[i].IgnoreAutoTag = policy
		}
	}

	// Append destination first so its spelling remains stable when canonical
	// names differ only by case. NormalizeAliases removes the final canonical
	// name and case-insensitive duplicates below.
	for _, p := range mergedPerformers {
		policy, _ := policyForName(p.Name)
		aliases = append(aliases, models.PerformerAlias{
			Alias:         p.Name,
			IgnoreAutoTag: policy,
		})
	}

	finalName := destination.Name
	if values.Name.Set {
		finalName = values.Name.Value
	}
	values.Aliases = &models.UpdatePerformerAliases{
		Values: performer.NormalizeAliases(finalName, aliases),
		Mode:   models.RelationshipUpdateModeSet,
	}

	// A caller that explicitly supplies the final canonical policy is making a
	// deliberate edit. Otherwise, selecting any existing canonical name or
	// alias adopts the policy already attached to that text value.
	if !values.IgnorePrimaryNameAutoTag.Set {
		if policy, ok := policyForName(finalName); ok {
			values.IgnorePrimaryNameAutoTag = models.NewOptionalBool(policy)
		}
	}

	return nil
}
