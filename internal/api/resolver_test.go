package api

import (
	"context"

	"github.com/99designs/gqlgen/graphql"
	"github.com/stashapp/stash/internal/manager"
	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/models/mocks"
	"github.com/stashapp/stash/pkg/plugin/hook"
	"github.com/vektah/gqlparser/v2/ast"
)

type bulkUpdateCall struct {
	description string
	ids         []int
	hookType    hook.TriggerEnum
	input       interface{}
	inputFields []string
}

type fakeBulkUpdater struct {
	nextID int
	calls  []bulkUpdateCall
}

func (f *fakeBulkUpdater) BulkUpdate(ctx context.Context, description string, ids []int, operation manager.BulkUpdateOperation, hookType hook.TriggerEnum, input interface{}, inputFields []string) int {
	if f.nextID == 0 {
		f.nextID = 1
	}
	id := f.nextID
	f.nextID++

	f.calls = append(f.calls, bulkUpdateCall{
		description: description,
		ids:         append([]int(nil), ids...),
		hookType:    hookType,
		input:       input,
		inputFields: append([]string(nil), inputFields...),
	})

	return id
}

// TODO - move this into a common area
func newResolver(db *mocks.Database) (*Resolver, *fakeBulkUpdater) {
	config.InitializeEmpty()

	bulkUpdater := &fakeBulkUpdater{}

	return &Resolver{
		repository:   db.Repository(),
		bulkUpdater:  bulkUpdater,
		hookExecutor: &mockHookExecutor{},
	}, bulkUpdater
}

var testCtx = context.Background()

type mockHookExecutor struct{}

func (*mockHookExecutor) ExecutePostHooks(ctx context.Context, id int, hookType hook.TriggerEnum, input interface{}, inputFields []string) {
}

func PtrString(s string) *string {
	return &s
}

func PtrInt(i int) *int {
	return &i
}

func PtrBool(b bool) *bool {
	return &b
}

func PtrSortDirectionEnum(e models.SortDirectionEnum) *models.SortDirectionEnum {
	return &e
}

func withGqlContext(ctx context.Context, inputMap map[string]interface{}) context.Context {
	rc := &graphql.OperationContext{
		Variables: inputMap,
	}
	ctx = graphql.WithOperationContext(ctx, rc)

	fc := &graphql.FieldContext{
		Field: graphql.CollectedField{
			Field: &ast.Field{
				Name: "test",
				Arguments: ast.ArgumentList{
					{
						Name: "input",
						Value: &ast.Value{
							Kind: ast.Variable,
							Raw:  "input",
						},
					},
				},
				Definition: &ast.FieldDefinition{
					Arguments: ast.ArgumentDefinitionList{
						{
							Name: updateInputField,
						},
					},
				},
			},
		},
	}
	return graphql.WithFieldContext(ctx, fc)
}
