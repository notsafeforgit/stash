package sqlite

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/stashapp/stash/pkg/models"
)

// astPredicate holds the compiled SQL fragments from a FilterAST node.
type astPredicate struct {
	joins         joins
	where         *sqlClause
	having        *sqlClause
	withClauses   []sqlClause
	recursiveWith bool
}

func (p *astPredicate) toFilterBuilder() *filterBuilder {
	ret := &filterBuilder{}
	if p == nil {
		return ret
	}

	ret.joins = p.joins
	if p.where != nil && p.where.sql != "" {
		ret.whereClauses = append(ret.whereClauses, *p.where)
	}
	if p.having != nil && p.having.sql != "" {
		ret.havingClauses = append(ret.havingClauses, *p.having)
	}
	ret.withClauses = append(ret.withClauses, p.withClauses...)
	ret.recursiveWith = p.recursiveWith
	return ret
}

func combineASTPredicates(operator models.FilterGroupOperator, left, right *astPredicate) *astPredicate {
	if left == nil {
		return right
	}
	if right == nil {
		return left
	}

	ret := &astPredicate{
		joins:         append(joins{}, left.joins...),
		withClauses:   append([]sqlClause{}, left.withClauses...),
		recursiveWith: left.recursiveWith || right.recursiveWith,
	}
	ret.joins.add(right.joins...)
	ret.withClauses = append(ret.withClauses, right.withClauses...)

	switch {
	case left.where != nil && right.where != nil:
		var clause sqlClause
		switch operator {
		case models.FilterGroupOperatorOr:
			clause = orClauses(*left.where, *right.where)
		default:
			clause = andClauses(*left.where, *right.where)
		}
		ret.where = &clause
	case left.where != nil:
		clause := *left.where
		ret.where = &clause
	case right.where != nil:
		clause := *right.where
		ret.where = &clause
	}

	switch {
	case left.having != nil && right.having != nil:
		var clause sqlClause
		switch operator {
		case models.FilterGroupOperatorOr:
			clause = orClauses(*left.having, *right.having)
		default:
			clause = andClauses(*left.having, *right.having)
		}
		ret.having = &clause
	case left.having != nil:
		clause := *left.having
		ret.having = &clause
	case right.having != nil:
		clause := *right.having
		ret.having = &clause
	}

	return ret
}

// conditionHandlerFn maps a FilterASTCondition to a criterionHandler.
type conditionHandlerFn func(condition *models.FilterASTCondition) (criterionHandler, error)

func compileASTNode(ctx context.Context, node *models.FilterASTNode, conditionFn conditionHandlerFn) (*astPredicate, error) {
	if err := node.Validate(); err != nil {
		return nil, err
	}

	if node.Condition != nil {
		return compileASTCondition(ctx, node.Condition, conditionFn)
	}

	return compileASTGroup(ctx, node.Group, conditionFn)
}

func compileASTGroup(ctx context.Context, group *models.FilterASTGroup, conditionFn conditionHandlerFn) (*astPredicate, error) {
	if err := group.Validate(); err != nil {
		return nil, err
	}

	var children []*astPredicate
	for _, child := range group.Children {
		predicate, err := compileASTNode(ctx, child, conditionFn)
		if err != nil {
			return nil, err
		}
		if predicate == nil {
			continue
		}
		children = append(children, predicate)
	}

	if len(children) == 0 {
		return nil, nil
	}

	current := children[0]
	for _, child := range children[1:] {
		current = combineASTPredicates(group.Operator, current, child)
	}

	return current, nil
}

func compileASTCondition(ctx context.Context, condition *models.FilterASTCondition, conditionFn conditionHandlerFn) (*astPredicate, error) {
	handler, err := conditionFn(condition)
	if err != nil {
		return nil, err
	}

	filter := filterBuilderFromHandler(ctx, handler)
	if err := filter.getError(); err != nil {
		return nil, err
	}
	if filter.subFilter != nil {
		return nil, fmt.Errorf("AST condition %q unexpectedly produced a sub-filter", condition.Field)
	}

	whereSQL, whereArgs := filter.generateWhereClauses()
	havingSQL, havingArgs := filter.generateHavingClauses()

	ret := &astPredicate{
		joins:         filter.getAllJoins(),
		withClauses:   append([]sqlClause{}, filter.withClauses...),
		recursiveWith: filter.recursiveWith,
	}
	if whereSQL != "" {
		ret.where = &sqlClause{sql: whereSQL, args: whereArgs}
	}
	if havingSQL != "" {
		ret.having = &sqlClause{sql: havingSQL, args: havingArgs}
	}

	return ret, nil
}

// decodeASTValue round-trips value through JSON to decode it into type T.
func decodeASTValue[T any](value interface{}) (T, error) {
	var ret T

	bytes, err := json.Marshal(value)
	if err != nil {
		return ret, err
	}

	if err := json.Unmarshal(bytes, &ret); err != nil {
		return ret, err
	}

	return ret, nil
}
