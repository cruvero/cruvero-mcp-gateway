package policy

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"strings"
)

// ValidateArguments performs basic JSON-schema required/type/enum checks.
func ValidateArguments(args map[string]any, schema json.RawMessage) []Violation {
	trimmed := strings.TrimSpace(string(schema))
	if len(schema) == 0 || trimmed == "" || trimmed == "null" || trimmed == "{}" {
		return nil
	}
	if args == nil {
		args = map[string]any{}
	}

	var raw map[string]any
	if err := json.Unmarshal(schema, &raw); err != nil {
		return []Violation{{
			Type:     ViolationSchemaViolation,
			Detail:   fmt.Sprintf("invalid schema: %v", err),
			Severity: SeverityMedium,
		}}
	}

	violations := make([]Violation, 0)
	violations = append(violations, validateRequired(args, raw["required"])...)
	violations = append(violations, validateProperties(args, raw["properties"])...)

	if len(violations) == 0 {
		return nil
	}
	return violations
}

func validateRequired(args map[string]any, requiredRaw any) []Violation {
	required, ok := requiredRaw.([]any)
	if !ok || len(required) == 0 {
		return nil
	}

	violations := make([]Violation, 0)
	for _, item := range required {
		field, ok := item.(string)
		if !ok {
			continue
		}
		name := strings.TrimSpace(field)
		if name == "" {
			continue
		}
		if _, exists := args[name]; exists {
			continue
		}
		violations = append(violations, Violation{
			Type:     ViolationSchemaViolation,
			Detail:   fmt.Sprintf("missing required argument: %s", name),
			Severity: SeverityMedium,
		})
	}
	return violations
}

func validateProperties(args map[string]any, propertiesRaw any) []Violation {
	properties, ok := propertiesRaw.(map[string]any)
	if !ok || len(properties) == 0 {
		return nil
	}

	violations := make([]Violation, 0)
	for name, value := range args {
		rawProp, ok := properties[name]
		if !ok {
			continue
		}
		prop, ok := rawProp.(map[string]any)
		if !ok {
			continue
		}

		if expectedType, ok := prop["type"].(string); ok {
			if !matchesJSONType(value, expectedType) {
				violations = append(violations, Violation{
					Type:     ViolationSchemaViolation,
					Detail:   fmt.Sprintf("argument %s has wrong type, expected %s", name, expectedType),
					Severity: SeverityMedium,
				})
			}
		}

		enumValues, ok := prop["enum"].([]any)
		if ok && len(enumValues) > 0 && !valueInEnum(value, enumValues) {
			violations = append(violations, Violation{
				Type:     ViolationSchemaViolation,
				Detail:   fmt.Sprintf("argument %s is not in enum", name),
				Severity: SeverityMedium,
			})
		}
	}
	return violations
}

func matchesJSONType(value any, expectedType string) bool {
	switch strings.TrimSpace(expectedType) {
	case "string":
		_, ok := value.(string)
		return ok
	case "boolean":
		_, ok := value.(bool)
		return ok
	case "number":
		_, ok := numericValue(value)
		return ok
	case "integer":
		number, ok := numericValue(value)
		return ok && number == math.Trunc(number)
	case "object":
		if value == nil {
			return false
		}
		kind := reflect.ValueOf(value).Kind()
		return kind == reflect.Map
	case "array":
		if value == nil {
			return false
		}
		kind := reflect.ValueOf(value).Kind()
		return kind == reflect.Slice || kind == reflect.Array
	default:
		return true
	}
}

func valueInEnum(value any, enumValues []any) bool {
	for _, candidate := range enumValues {
		if reflect.DeepEqual(value, candidate) {
			return true
		}

		left, leftOK := numericValue(value)
		right, rightOK := numericValue(candidate)
		if leftOK && rightOK && left == right {
			return true
		}

		leftString, leftIsString := value.(string)
		rightString, rightIsString := candidate.(string)
		if leftIsString && rightIsString && leftString == rightString {
			return true
		}
	}
	return false
}

func numericValue(value any) (float64, bool) {
	switch typed := value.(type) {
	case int:
		return float64(typed), true
	case int8:
		return float64(typed), true
	case int16:
		return float64(typed), true
	case int32:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case uint:
		return float64(typed), true
	case uint8:
		return float64(typed), true
	case uint16:
		return float64(typed), true
	case uint32:
		return float64(typed), true
	case uint64:
		return float64(typed), true
	case float32:
		return float64(typed), true
	case float64:
		return typed, true
	case json.Number:
		v, err := typed.Float64()
		if err != nil {
			return 0, false
		}
		return v, true
	default:
		return 0, false
	}
}
