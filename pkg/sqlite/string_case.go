package sqlite

import (
	"regexp"
	"strings"
)

// stringLikeClause preserves LIKE's percent/underscore wildcards and NULL
// semantics. An explicit case setting uses the cached Unicode-aware matcher;
// omitted settings retain the behavior of existing saved queries. Never change
// a connection-wide PRAGMA: adjacent AST conditions can have different settings.
func stringLikeClause(column, pattern string, not bool, caseSensitive *bool) sqlClause {
	op := " LIKE ?"
	if caseSensitive != nil {
		var b strings.Builder
		b.WriteString(`\A(?s:`)
		for _, r := range pattern {
			switch r {
			case '%':
				b.WriteString(".*")
			case '_':
				b.WriteByte('.')
			default:
				b.WriteString(regexp.QuoteMeta(string(r)))
			}
		}
		b.WriteString(`)\z`)
		pattern = stringRegexPattern(b.String(), caseSensitive)
		op = " regexp ?"
	}
	if not {
		op = " NOT" + op
	}
	if caseSensitive != nil {
		// regexp's registered Go callback takes a string, so protect NULL inputs
		// while retaining SQL's three-valued logic (also beneath NOT groups).
		return makeClause("CASE WHEN "+column+" IS NULL THEN NULL ELSE "+column+op+" END", pattern)
	}
	return makeClause(column+op, pattern)
}

func stringRegexPattern(pattern string, caseSensitive *bool) string {
	if caseSensitive == nil {
		return pattern
	}
	if *caseSensitive {
		return "(?-i:" + pattern + ")"
	}
	return "(?i:" + pattern + ")"
}

func customFieldEqualityClause(column string, values []any, caseSensitive *bool) sqlClause {
	if len(values) == 0 {
		return makeClause("0")
	}
	clauses := make([]sqlClause, 0, len(values))
	for _, value := range values {
		if text, ok := value.(string); ok && !*caseSensitive {
			// Custom-field equality has no LIKE wildcards. Preserve exact text,
			// including percent/underscore, and its distinction from numeric values.
			pattern := stringRegexPattern(`\A`+regexp.QuoteMeta(text)+`\z`, caseSensitive)
			clauses = append(clauses, makeClause("CASE WHEN typeof("+column+")='text' THEN "+column+" regexp ? ELSE 0 END", pattern))
		} else {
			clauses = append(clauses, makeClause(column+" = ?", value))
		}
	}
	return orClauses(clauses...)
}
