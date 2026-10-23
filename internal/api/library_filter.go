package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"

	"github.com/99designs/gqlgen/graphql"
	"github.com/stashapp/stash/pkg/entityid"
	"github.com/stashapp/stash/pkg/models"
)

func MarshalWireAny(value any) graphql.ContextMarshaler {
	return graphql.ContextWriterFunc(func(_ context.Context, w io.Writer) error { graphql.MarshalAny(value).MarshalGQL(w); return nil })
}
func UnmarshalWireAny(ctx context.Context, value any) (any, error) {
	location, err := libraryLocation(ctx, true)
	if err != nil {
		return nil, err
	}
	if location.directives.ForName("libraryFilter") == nil {
		return value, nil
	}
	field, _ := location.parent["field"].(string)
	return libraryCriterion(ctx, location.scope, field, value, true)
}

func libraryCriterion(ctx context.Context, scope, field string, value any, input bool) (any, error) {
	return entityid.MapCriterionIDs(scope, field, value, func(kind string, raw any) (any, error) {
		return libraryCriterionIDs(ctx, models.ArchiveEntityKind(kind), raw, input, field == "id")
	})
}

func libraryCriterionIDs(ctx context.Context, kind models.ArchiveEntityKind, raw any, input, numeric bool) (any, error) {
	if input {
		id, ok := raw.(string)
		if !ok {
			return nil, errors.New("library filter ID must be a UUID")
		}
		local, err := entityid.FilterKey(ctx, kind, id)
		if err != nil {
			return nil, err
		}
		if storingLibraryFilter(ctx) {
			return id, nil
		}
		if numeric {
			return local, nil
		}
		return strconv.Itoa(local), nil
	}
	var local int
	switch v := raw.(type) {
	case string:
		if strings.Contains(v, "-") {
			return v, nil
		}
		local, _ = strconv.Atoi(v)
	case float64:
		local = int(v)
	case json.Number:
		local, _ = strconv.Atoi(string(v))
	case int:
		local = v
	}
	if local < 1 {
		return nil, errors.New("invalid stored library filter ID")
	}
	return entityid.Public(ctx, kind, local)
}

func publicFilterAST(ctx context.Context, mode models.FilterMode, node map[string]any) (map[string]any, error) {
	return entityid.MapFilterAST(libraryModeKind(string(mode)), node, func(kind string, raw any) (any, error) {
		return libraryCriterionIDs(ctx, models.ArchiveEntityKind(kind), raw, false, false)
	})
}

func storingLibraryFilter(ctx context.Context) bool {
	field := graphql.GetFieldContext(ctx)
	if field == nil || field.Field.Field == nil {
		return false
	}
	return field.Field.Name == "saveFilter" || field.Field.Name == "configureDefaultFilter" || field.Field.Name == "mediaShareCreate"
}
