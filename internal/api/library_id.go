package api

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"io"
	"strconv"
	"strings"

	"github.com/99designs/gqlgen/graphql"
	"github.com/stashapp/stash/pkg/entityid"
	"github.com/stashapp/stash/pkg/models"
	"github.com/vektah/gqlparser/v2/ast"
)

// The schema marks library IDs explicitly. Other IDs (plugins, jobs and remote
// providers) have their own contracts and must not be interpreted as local keys.
// These scalar functions are used by gqlgen for string, int, FileID and FolderID
// storage representations; all four have the same public UUID representation.
func MarshalWireID(value string) graphql.ContextMarshaler {
	return graphql.ContextWriterFunc(func(ctx context.Context, w io.Writer) error {
		output := value
		kind, marked, _, err := libraryIDScope(ctx, false)
		if err != nil {
			return err
		}
		if marked {
			if public, parseErr := uuid.Parse(value); parseErr == nil && public != uuid.Nil && public.String() == value {
				// Frozen references (shares/cover origins) already carry their UUID,
				// including identities that no longer have an active library row.
				graphql.MarshalID(value).MarshalGQL(w)
				return nil
			}
			id, err := strconv.Atoi(value)
			if err != nil {
				return errors.New("invalid internal library identity")
			}
			output, err = entityid.Public(ctx, kind, id)
			if err != nil {
				return err
			}
		}
		graphql.MarshalID(output).MarshalGQL(w)
		return nil
	})
}

func UnmarshalWireID(ctx context.Context, v any) (string, error) {
	kind, marked, nullable, err := libraryIDScope(ctx, true)
	if err != nil {
		return "", err
	}
	if !marked {
		return graphql.UnmarshalID(v)
	}
	value, ok := v.(string)
	if !ok {
		return "", errors.New("library ID must be a UUID string")
	}
	// Nullable relationship inputs historically support an empty string to clear.
	if value == "" && nullable {
		return "", nil
	}
	lookup := entityid.Local
	field := graphql.GetFieldContext(ctx)
	if field != nil && field.Object == "Query" && strings.HasPrefix(field.Field.Name, "find") {
		lookup = entityid.FilterKey
	}
	id, err := lookup(ctx, kind, value)
	if err != nil {
		return "", err
	}
	if kind == "" {
		return value, nil
	}
	return strconv.Itoa(id), nil
}

func MarshalWireIntID(value int) graphql.ContextMarshaler { return MarshalWireID(strconv.Itoa(value)) }
func UnmarshalWireIntID(ctx context.Context, v any) (int, error) {
	s, err := UnmarshalWireID(ctx, v)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(s)
}
func MarshalWireFileID(value models.FileID) graphql.ContextMarshaler {
	return MarshalWireIntID(int(value))
}
func UnmarshalWireFileID(ctx context.Context, v any) (models.FileID, error) {
	id, err := UnmarshalWireIntID(ctx, v)
	return models.FileID(id), err
}
func MarshalWireFolderID(value models.FolderID) graphql.ContextMarshaler {
	return MarshalWireIntID(int(value))
}
func UnmarshalWireFolderID(ctx context.Context, v any) (models.FolderID, error) {
	id, err := UnmarshalWireIntID(ctx, v)
	return models.FolderID(id), err
}

type librarySchema struct {
	graphql.ExecutableSchema
	repo models.Repository
}
type librarySchemaKey struct{}

func (r *Resolver) publicID(ctx context.Context, kind models.ArchiveEntityKind, id int) (string, error) {
	if _, err := entityid.From(ctx); err != nil {
		ctx = entityid.WithRepository(ctx, r.repository)
	}
	return entityid.Public(ctx, kind, id)
}

// Only previously issued media URLs and bookmark resolution accept local keys.
// New application operations accept UUIDs exclusively.
func mediaLibraryKey(ctx context.Context, kind models.ArchiveEntityKind, value string) (int, error) {
	if id, err := strconv.Atoi(value); err == nil && id > 0 {
		return id, nil
	}
	return entityid.Local(ctx, kind, value)
}

func newLibrarySchema(config Config) graphql.ExecutableSchema {
	r := config.Resolvers.(*Resolver)
	return &librarySchema{ExecutableSchema: NewExecutableSchema(config), repo: r.repository}
}

// Find-list arguments retain their nonempty selection even when every UUID is
// missing. Remove the nonmatching sentinels only at the repository lookup;
// passing them to FindMany would report an internal storage key to the client.
func availableLibraryKeys[T ~int | ~int32](keys []T) []T {
	available := make([]T, 0, len(keys))
	for _, key := range keys {
		if key > 0 {
			available = append(available, key)
		}
	}
	return available
}

func (s *librarySchema) Exec(ctx context.Context) graphql.ResponseHandler {
	resolver := entityid.NewResolver(s.repo, graphql.GetOperationContext(ctx).Operation.Operation == ast.Query)
	ctx = entityid.WithResolver(ctx, resolver)
	ctx = context.WithValue(ctx, librarySchemaKey{}, s.Schema())
	next := s.ExecutableSchema.Exec(ctx)
	return func(responseCtx context.Context) *graphql.Response {
		// Retain the operation-scoped resolver across websocket/subscription events.
		responseCtx = context.WithValue(responseCtx, librarySchemaKey{}, s.Schema())
		return next(entityid.WithResolver(responseCtx, resolver))
	}
}

func libraryScope(directives ast.DirectiveList, current string, raw any) string {
	d := directives.ForName("libraryScope")
	if d == nil {
		return current
	}
	if arg := d.Arguments.ForName("kind"); arg != nil {
		return arg.Value.Raw
	}
	if arg := d.Arguments.ForName("from"); arg != nil {
		if values, ok := raw.(map[string]any); ok {
			name, _ := values[arg.Value.Raw].(string)
			return libraryModeKind(name)
		}
	}
	return ""
}

func libraryModeKind(mode string) string {
	return entityid.ModeKind(mode)
}

type wireLocation struct {
	directives ast.DirectiveList
	scope      string
	nullable   bool
	parent     map[string]any
}

func libraryLocation(ctx context.Context, input bool) (wireLocation, error) {
	fc := graphql.GetFieldContext(ctx)
	if fc == nil || fc.Field.Field == nil || fc.Field.Definition == nil {
		return wireLocation{}, nil
	}
	scope := ""
	var parents []*graphql.FieldContext
	for p := fc; p != nil; p = p.Parent {
		parents = append(parents, p)
	}
	for i := len(parents) - 1; i >= 0; i-- {
		p := parents[i]
		if p.Field.Field != nil && p.Field.Definition != nil {
			scope = libraryScope(p.Field.Definition.Directives, scope, p.Field.ArgumentMap(graphql.GetOperationContext(ctx).Variables))
		}
	}
	var parent map[string]any
	directives := fc.Field.Definition.Directives
	typeRef := fc.Field.Definition.Type
	if input {
		var steps []*graphql.PathContext
		for p := graphql.GetPathContext(ctx); p != nil; p = p.Parent {
			steps = append(steps, p)
		}
		schema, _ := ctx.Value(librarySchemaKey{}).(*ast.Schema)
		if schema == nil {
			return wireLocation{}, errors.New("library schema unavailable")
		}
		var raw any = fc.Field.ArgumentMap(graphql.GetOperationContext(ctx).Variables)
		first := true
		for i := len(steps) - 1; i >= 0; i-- {
			step := steps[i]
			if step.Index != nil {
				if list, ok := raw.([]any); ok && *step.Index < len(list) {
					raw = list[*step.Index]
				}
				if typeRef != nil && typeRef.Elem != nil {
					typeRef = typeRef.Elem
				}
				continue
			}
			if step.Field == nil {
				continue
			}
			name := *step.Field
			if first {
				arg := fc.Field.Definition.Arguments.ForName(name)
				if arg == nil {
					return wireLocation{}, errors.New("unknown library argument path")
				}
				typeRef, directives = arg.Type, arg.Directives
				first = false
			} else {
				def := schema.Types[typeRef.Name()]
				if def == nil {
					return wireLocation{}, errors.New("unknown library input type")
				}
				scope = libraryScope(def.Directives, scope, raw)
				field := def.Fields.ForName(name)
				if field == nil {
					return wireLocation{}, errors.New("unknown library input field")
				}
				typeRef, directives = field.Type, field.Directives
			}
			if obj, ok := raw.(map[string]any); ok {
				parent = obj
				raw = obj[name]
			}
			scope = libraryScope(directives, scope, raw)
		}
	}
	return wireLocation{directives: directives, scope: scope, nullable: !typeRef.NonNull, parent: parent}, nil
}

func libraryIDScope(ctx context.Context, input bool) (models.ArchiveEntityKind, bool, bool, error) {
	location, err := libraryLocation(ctx, input)
	if err != nil {
		return "", false, false, err
	}
	d := location.directives.ForName("libraryID")
	if d == nil {
		return "", false, false, nil
	}
	kind := d.Arguments.ForName("kind").Value.Raw
	if kind == "scope" {
		kind = location.scope
	}
	if kind == "" {
		return "", true, false, errors.New("library ID has no entity kind")
	}
	if kind == "any" {
		kind = ""
	}
	return models.ArchiveEntityKind(kind), true, location.nullable, nil
}
