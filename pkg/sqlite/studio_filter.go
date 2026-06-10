package sqlite

import (
	"context"
	"fmt"

	"github.com/stashapp/stash/pkg/models"
)

// Limits traversal of malformed cyclic studio parent graphs. This is separate
// from the user-facing FilterAST group nesting limit.
const maxStudioHierarchyTraversalDepth = 100

type studioFilterHandler struct {
	studioFilter *models.StudioFilterType
}

func (qb *studioFilterHandler) validate() error {
	studioFilter := qb.studioFilter
	if studioFilter == nil {
		return nil
	}

	if err := validateFilterCombination(studioFilter.OperatorFilter); err != nil {
		return err
	}

	if subFilter := studioFilter.SubFilter(); subFilter != nil {
		sqb := &studioFilterHandler{studioFilter: subFilter}
		if err := sqb.validate(); err != nil {
			return err
		}
	}

	return nil
}

func (qb *studioFilterHandler) handle(ctx context.Context, f *filterBuilder) {
	studioFilter := qb.studioFilter
	if studioFilter == nil {
		return
	}

	if err := qb.validate(); err != nil {
		f.setError(err)
		return
	}

	f.handleCriterion(ctx, qb.criterionHandler())

	sf := studioFilter.SubFilter()
	if sf != nil {
		sub := &studioFilterHandler{sf}
		handleSubFilter(ctx, sub, f, studioFilter.OperatorFilter)
	}
}

func (qb *studioFilterHandler) criterionHandler() criterionHandler {
	studioFilter := qb.studioFilter
	return compoundHandler{
		stringCriterionHandler(studioFilter.Name, studioTable+".name"),
		stringCriterionHandler(studioFilter.Details, studioTable+".details"),
		qb.urlsCriterionHandler(studioFilter.URL),
		intCriterionHandler(studioFilter.Rating100, studioTable+".rating", nil),
		boolCriterionHandler(studioFilter.Favorite, studioTable+".favorite", nil),
		boolCriterionHandler(studioFilter.IgnoreAutoTag, studioTable+".ignore_auto_tag", nil),
		boolCriterionHandler(studioFilter.Organized, studioTable+".organized", nil),

		criterionHandlerFunc(func(ctx context.Context, f *filterBuilder) {
			if studioFilter.StashID != nil {
				studioRepository.stashIDs.leftJoin(f, "studio_stash_ids", "studios.id")
				stringCriterionHandler(studioFilter.StashID, "studio_stash_ids.stash_id")(ctx, f)
			}
		}),
		&stashIDCriterionHandler{
			c:                 studioFilter.StashIDEndpoint,
			stashIDRepository: &studioRepository.stashIDs,
			stashIDTableAs:    "studio_stash_ids",
			parentIDCol:       "studios.id",
		},
		&stashIDsCriterionHandler{
			c:                 studioFilter.StashIDsEndpoint,
			stashIDRepository: &studioRepository.stashIDs,
			stashIDTableAs:    "studio_stash_ids",
			parentIDCol:       "studios.id",
		},

		qb.isMissingCriterionHandler(studioFilter.IsMissing),
		qb.tagCountCriterionHandler(studioFilter.TagCount),
		qb.sceneCountCriterionHandler(studioFilter.SceneCount),
		qb.imageCountCriterionHandler(studioFilter.ImageCount),
		qb.galleryCountCriterionHandler(studioFilter.GalleryCount),
		qb.groupCountCriterionHandler(studioFilter.GroupCount),
		qb.parentCriterionHandler(studioFilter.Parents),
		qb.aliasCriterionHandler(studioFilter.Aliases),
		qb.tagsCriterionHandler(studioFilter.Tags),
		qb.ancestorTagsCriterionHandler(studioFilter.AncestorTags),
		qb.descendantTagsCriterionHandler(studioFilter.DescendantTags),
		qb.childCountCriterionHandler(studioFilter.ChildCount),
		&timestampCriterionHandler{studioFilter.CreatedAt, studioTable + ".created_at", nil},
		&timestampCriterionHandler{studioFilter.UpdatedAt, studioTable + ".updated_at", nil},

		&relatedFilterHandler{
			relatedIDCol:   "scenes.id",
			relatedRepo:    sceneRepository.repository,
			relatedHandler: &sceneFilterHandler{studioFilter.ScenesFilter},
			joinFn: func(f *filterBuilder) {
				studioRepository.scenes.innerJoin(f, "", "studios.id")
			},
		},

		&relatedFilterHandler{
			relatedIDCol:   "images.id",
			relatedRepo:    imageRepository.repository,
			relatedHandler: &imageFilterHandler{studioFilter.ImagesFilter},
			joinFn: func(f *filterBuilder) {
				studioRepository.images.innerJoin(f, "", "studios.id")
			},
		},

		&relatedFilterHandler{
			relatedIDCol:   "galleries.id",
			relatedRepo:    galleryRepository.repository,
			relatedHandler: &galleryFilterHandler{studioFilter.GalleriesFilter},
			joinFn: func(f *filterBuilder) {
				studioRepository.galleries.innerJoin(f, "", "studios.id")
			},
		},

		&relatedFilterHandler{
			relatedIDCol:   "groups.id",
			relatedRepo:    groupRepository.repository,
			relatedHandler: &groupFilterHandler{studioFilter.GroupsFilter},
			joinFn: func(f *filterBuilder) {
				studioRepository.groups.innerJoin(f, "", "studios.id")
			},
		},

		&customFieldsFilterHandler{
			table: studiosCustomFieldsTable.GetTable(),
			fkCol: studioIDColumn,
			c:     studioFilter.CustomFields,
			idCol: "studios.id",
		},
	}
}

func (qb *studioFilterHandler) isMissingCriterionHandler(isMissing *string) criterionHandlerFunc {
	return func(ctx context.Context, f *filterBuilder) {
		if isMissing != nil && *isMissing != "" {
			switch *isMissing {
			case "url":
				studiosURLsTableMgr.leftJoin(f, "", "studios.id")
				f.addWhere("studio_urls.url IS NULL")
			case "image":
				f.addWhere("studios.image_blob IS NULL")
			case "stash_id":
				studioRepository.stashIDs.leftJoin(f, "studio_stash_ids", "studios.id")
				f.addWhere("studio_stash_ids.studio_id IS NULL")
			case "aliases":
				studiosAliasesTableMgr.leftJoin(f, "", "studios.id")
				f.addWhere("studio_aliases.alias IS NULL")
			case "tags":
				f.addLeftJoin(studiosTagsTable, "tags_join", "tags_join.studio_id = studios.id")
				f.addWhere("tags_join.studio_id IS NULL")
			default:
				if err := validateIsMissing(*isMissing, []string{
					"details", "rating",
				}); err != nil {
					f.setError(err)
					return
				}
				f.addWhere("(studios." + *isMissing + " IS NULL OR TRIM(studios." + *isMissing + ") = '')")
			}
		}
	}
}

func (qb *studioFilterHandler) sceneCountCriterionHandler(sceneCount *models.IntCriterionInput) criterionHandlerFunc {
	return func(ctx context.Context, f *filterBuilder) {
		if sceneCount != nil {
			f.addLeftJoin("scenes", "", "scenes.studio_id = studios.id")
			clause, args := getIntCriterionWhereClause("count(distinct scenes.id)", *sceneCount)

			f.addHaving(clause, args...)
		}
	}
}

func (qb *studioFilterHandler) imageCountCriterionHandler(imageCount *models.IntCriterionInput) criterionHandlerFunc {
	return func(ctx context.Context, f *filterBuilder) {
		if imageCount != nil {
			f.addLeftJoin("images", "", "images.studio_id = studios.id")
			clause, args := getIntCriterionWhereClause("count(distinct images.id)", *imageCount)

			f.addHaving(clause, args...)
		}
	}
}

func (qb *studioFilterHandler) galleryCountCriterionHandler(galleryCount *models.IntCriterionInput) criterionHandlerFunc {
	return func(ctx context.Context, f *filterBuilder) {
		if galleryCount != nil {
			f.addLeftJoin("galleries", "", "galleries.studio_id = studios.id")
			clause, args := getIntCriterionWhereClause("count(distinct galleries.id)", *galleryCount)

			f.addHaving(clause, args...)
		}
	}
}

func (qb *studioFilterHandler) groupCountCriterionHandler(groupCount *models.IntCriterionInput) criterionHandlerFunc {
	return func(ctx context.Context, f *filterBuilder) {
		if groupCount != nil {
			f.addLeftJoin("groups", "", "groups.studio_id = studios.id")
			clause, args := getIntCriterionWhereClause("count(distinct groups.id)", *groupCount)

			f.addHaving(clause, args...)
		}
	}
}

func (qb *studioFilterHandler) tagCountCriterionHandler(tagCount *models.IntCriterionInput) criterionHandlerFunc {
	h := countCriterionHandlerBuilder{
		primaryTable: studioTable,
		joinTable:    studiosTagsTable,
		primaryFK:    studioIDColumn,
	}

	return h.handler(tagCount)
}

func (qb *studioFilterHandler) parentCriterionHandler(parents *models.MultiCriterionInput) criterionHandlerFunc {
	addJoinsFunc := func(f *filterBuilder, joinType joinType) {
		f.addJoin(joinType, "studios", "parent_studio", "parent_studio.id = studios.parent_id")
	}
	h := multiCriterionHandlerBuilder{
		primaryTable: studioTable,
		foreignTable: "parent_studio",
		joinTable:    "",
		primaryFK:    studioIDColumn,
		foreignFK:    "parent_id",
		addJoinsFunc: addJoinsFunc,
	}
	return h.handler(parents)
}

func (qb *studioFilterHandler) aliasCriterionHandler(alias *models.StringCriterionInput) criterionHandlerFunc {
	h := stringListCriterionHandlerBuilder{
		primaryTable: studioTable,
		primaryFK:    studioIDColumn,
		joinTable:    studioAliasesTable,
		stringColumn: studioAliasColumn,
		addJoinTable: func(f *filterBuilder, joinType joinType) {
			studiosAliasesTableMgr.join(f, joinType, "", "studios.id")
		},
	}

	return h.handler(alias)
}

func (qb *studioFilterHandler) urlsCriterionHandler(url *models.StringCriterionInput) criterionHandlerFunc {
	h := stringListCriterionHandlerBuilder{
		primaryTable: studioTable,
		primaryFK:    studioIDColumn,
		joinTable:    studioURLsTable,
		stringColumn: studioURLColumn,
		addJoinTable: func(f *filterBuilder, joinType joinType) {
			studiosURLsTableMgr.join(f, joinType, "", "studios.id")
		},
	}

	return h.handler(url)
}

func (qb *studioFilterHandler) childCountCriterionHandler(childCount *models.IntCriterionInput) criterionHandlerFunc {
	return func(ctx context.Context, f *filterBuilder) {
		if childCount != nil {
			f.addLeftJoin("studios", "children_count", "children_count.parent_id = studios.id")
			clause, args := getIntCriterionWhereClause("count(distinct children_count.id)", *childCount)

			f.addHaving(clause, args...)
		}
	}
}

func (qb *studioFilterHandler) tagsCriterionHandler(tags *models.HierarchicalMultiCriterionInput) criterionHandlerFunc {
	h := joinedHierarchicalMultiCriterionHandlerBuilder{
		primaryTable: studioTable,
		foreignTable: tagTable,
		foreignFK:    "tag_id",

		relationsTable: "tags_relations",
		joinTable:      studiosTagsTable,
		joinAs:         "studio_tag",
		primaryFK:      studioIDColumn,
	}

	return h.handler(tags)
}

func (qb *studioFilterHandler) ancestorTagsCriterionHandler(tags *models.HierarchicalMultiCriterionInput) criterionHandlerFunc {
	return studioHierarchyTagsCriterionHandler(tags, "ancestor")
}

func (qb *studioFilterHandler) descendantTagsCriterionHandler(tags *models.HierarchicalMultiCriterionInput) criterionHandlerFunc {
	return studioHierarchyTagsCriterionHandler(tags, "descendant")
}

func studioHierarchyTagsCriterionHandler(tags *models.HierarchicalMultiCriterionInput, direction string) criterionHandlerFunc {
	return func(ctx context.Context, f *filterBuilder) {
		if tags == nil {
			return
		}

		criterion := *tags
		if criterion.Modifier == models.CriterionModifierEquals && criterion.Depth != nil && *criterion.Depth != 0 {
			f.setError(fmt.Errorf("depth is not supported for equals modifier in hierarchical multi criterion input"))
			return
		}

		if criterion.Modifier == models.CriterionModifierExcludes {
			criterion.Modifier = models.CriterionModifierIncludesAll
			criterion.Excludes = append(criterion.Excludes, criterion.Value...)
			criterion.Value = nil
		}

		if criterion.Modifier == models.CriterionModifierIsNull || criterion.Modifier == models.CriterionModifierNotNull {
			var notClause string
			if criterion.Modifier == models.CriterionModifierNotNull {
				notClause = "NOT"
			}

			joinTable := studioHierarchyTagsJoinTable(direction, "")
			joinAlias := fmt.Sprintf("%s_studio_tag", direction)
			f.addRecursiveWith(studioHierarchyCTE(direction), maxStudioHierarchyTraversalDepth)
			f.addLeftJoin(joinTable, joinAlias, fmt.Sprintf("%s.studio_id = studios.id", joinAlias))
			f.addWhere(fmt.Sprintf("%s.studio_id IS %s NULL", joinAlias, notClause))
			return
		}

		if len(criterion.Value) == 0 && len(criterion.Excludes) == 0 {
			return
		}

		if len(criterion.Value) > 0 {
			valuesClause, err := getHierarchicalValues(ctx, criterion.Value, tagTable, "tags_relations", "", "", criterion.Depth)
			if err != nil {
				f.setError(err)
				return
			}

			joinAlias := fmt.Sprintf("%s_studio_tag", direction)
			f.addRecursiveWith(studioHierarchyCTE(direction), maxStudioHierarchyTraversalDepth)
			f.addLeftJoin(
				studioHierarchyTagsJoinTable(direction, valuesClause),
				joinAlias,
				fmt.Sprintf("%s.studio_id = studios.id", joinAlias),
			)
			addHierarchicalConditionClauses(f, criterion, joinAlias, "root_id")
		}

		if len(criterion.Excludes) > 0 {
			valuesClause, err := getHierarchicalValues(ctx, criterion.Excludes, tagTable, "tags_relations", "", "", criterion.Depth)
			if err != nil {
				f.setError(err)
				return
			}

			joinAlias := fmt.Sprintf("%s_studio_tag_exclude", direction)
			f.addRecursiveWith(studioHierarchyCTE(direction), maxStudioHierarchyTraversalDepth)
			f.addLeftJoin(
				studioHierarchyTagsJoinTable(direction, valuesClause),
				joinAlias,
				fmt.Sprintf("%s.studio_id = studios.id", joinAlias),
			)

			criterionCopy := criterion
			criterionCopy.Modifier = models.CriterionModifierExcludes
			criterionCopy.Value = criterion.Excludes
			addHierarchicalConditionClauses(f, criterionCopy, joinAlias, "root_id")
		}
	}
}

func studioHierarchyCTE(direction string) string {
	switch direction {
	case "ancestor":
		return `studio_ancestor_hierarchy(studio_id, related_id, depth, path) AS (
SELECT id AS studio_id, parent_id AS related_id, 0 AS depth, ',' || id || ',' AS path FROM studios WHERE parent_id IS NOT NULL
UNION ALL
SELECT h.studio_id, s.parent_id, h.depth + 1, h.path || s.id || ','
FROM studio_ancestor_hierarchy h
INNER JOIN studios s ON s.id = h.related_id
WHERE s.parent_id IS NOT NULL
  AND h.depth < ?
  AND instr(h.path, ',' || s.id || ',') = 0
)`
	case "descendant":
		return `studio_descendant_hierarchy(studio_id, related_id, depth, path) AS (
SELECT p.id AS studio_id, c.id AS related_id, 0 AS depth, ',' || p.id || ',' AS path
FROM studios p
INNER JOIN studios c ON c.parent_id = p.id
UNION ALL
SELECT h.studio_id, c.id, h.depth + 1, h.path || c.id || ','
FROM studio_descendant_hierarchy h
INNER JOIN studios c ON c.parent_id = h.related_id
WHERE h.depth < ?
  AND instr(h.path, ',' || c.id || ',') = 0
)`
	default:
		panic("unsupported studio hierarchy direction")
	}
}

func studioHierarchyTagsJoinTable(direction, valuesClause string) string {
	hierarchyTable := fmt.Sprintf("studio_%s_hierarchy", direction)

	if valuesClause == "" {
		return fmt.Sprintf(`(
SELECT DISTINCT h.studio_id
FROM %s h
INNER JOIN studios_tags st ON st.studio_id = h.related_id
)`, hierarchyTable)
	}

	return fmt.Sprintf(`(
SELECT h.studio_id, t.column1 AS root_id, t.column2 AS item_id
FROM %s h
INNER JOIN studios_tags st ON st.studio_id = h.related_id
INNER JOIN (%s) t ON t.column2 = st.tag_id
)`, hierarchyTable, valuesClause)
}
