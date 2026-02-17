package policy

import (
	"fmt"
	"reflect"
	"regexp"
)

// DefaultDangerousPatterns are high-risk command patterns blocked by policy checks.
var DefaultDangerousPatterns = []string{
	`(?i)rm\s+-rf\s+/`,
	`(?i)\bsudo\s+`,
	`(?i);\s*(sh|bash)\b`,
	`(?i)curl.*\|\s*(sh|bash)`,
	`(?i)>(/dev/tcp|/dev/udp)`,
	`(?i)chmod\s+777`,
	`(?i)eval\s*\(`,
	`(?i)\bexec\b.*\bsh\b`,
}

// CompilePatterns compiles regex patterns for runtime checks.
func CompilePatterns(patterns []string) ([]*regexp.Regexp, error) {
	compiled := make([]*regexp.Regexp, 0, len(patterns))
	for _, pattern := range patterns {
		re, err := regexp.Compile(pattern)
		if err != nil {
			return nil, fmt.Errorf("compile dangerous pattern %q: %w", pattern, err)
		}
		compiled = append(compiled, re)
	}
	return compiled, nil
}

// CheckDangerous scans all nested string values in args against dangerous patterns.
func CheckDangerous(args map[string]any, patterns []*regexp.Regexp) []Violation {
	if len(args) == 0 || len(patterns) == 0 {
		return nil
	}

	values := make([]string, 0)
	for _, value := range args {
		collectStrings(value, &values)
	}

	violations := make([]Violation, 0)
	for _, value := range values {
		for _, pattern := range patterns {
			if pattern == nil {
				continue
			}
			if pattern.MatchString(value) {
				violations = append(violations, Violation{
					Type:     ViolationDangerousPattern,
					Detail:   fmt.Sprintf("dangerous pattern matched: %s", pattern.String()),
					Severity: SeverityCritical,
				})
			}
		}
	}

	if len(violations) == 0 {
		return nil
	}
	return violations
}

func collectStrings(value any, out *[]string) {
	if out == nil || value == nil {
		return
	}

	switch typed := value.(type) {
	case string:
		*out = append(*out, typed)
		return
	case []string:
		*out = append(*out, typed...)
		return
	case []any:
		for _, item := range typed {
			collectStrings(item, out)
		}
		return
	case map[string]any:
		for _, item := range typed {
			collectStrings(item, out)
		}
		return
	}

	rv := reflect.ValueOf(value)
	switch rv.Kind() {
	case reflect.Pointer, reflect.Interface:
		if !rv.IsNil() {
			collectStrings(rv.Elem().Interface(), out)
		}
	case reflect.Slice, reflect.Array:
		for i := 0; i < rv.Len(); i++ {
			collectStrings(rv.Index(i).Interface(), out)
		}
	case reflect.Map:
		iter := rv.MapRange()
		for iter.Next() {
			collectStrings(iter.Value().Interface(), out)
		}
	}
}
