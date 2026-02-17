package policy

import (
	"encoding/json"
	"testing"
)

func TestValidateArgumentsRequiredMissing(t *testing.T) {
	t.Parallel()

	args := map[string]any{"other": "x"}
	schema := json.RawMessage(`{
		"type":"object",
		"required":["name"],
		"properties":{"name":{"type":"string"}}
	}`)

	violations := ValidateArguments(args, schema)
	if len(violations) == 0 {
		t.Fatal("expected required-field violation")
	}
	if violations[0].Type != ViolationSchemaViolation {
		t.Fatalf("expected schema violation type, got %s", violations[0].Type)
	}
}

func TestValidateArgumentsTypeMismatch(t *testing.T) {
	t.Parallel()

	args := map[string]any{"count": "not-a-number"}
	schema := json.RawMessage(`{
		"type":"object",
		"properties":{"count":{"type":"integer"}}
	}`)

	violations := ValidateArguments(args, schema)
	if len(violations) == 0 {
		t.Fatal("expected type mismatch violation")
	}
}

func TestValidateArgumentsEnumMismatch(t *testing.T) {
	t.Parallel()

	args := map[string]any{"mode": "danger"}
	schema := json.RawMessage(`{
		"type":"object",
		"properties":{"mode":{"type":"string","enum":["safe","strict"]}}
	}`)

	violations := ValidateArguments(args, schema)
	if len(violations) == 0 {
		t.Fatal("expected enum violation")
	}
}

func TestValidateArgumentsValid(t *testing.T) {
	t.Parallel()

	args := map[string]any{
		"name":  "tool",
		"count": 3.0,
		"mode":  "safe",
	}
	schema := json.RawMessage(`{
		"type":"object",
		"required":["name","count"],
		"properties":{
			"name":{"type":"string"},
			"count":{"type":"integer"},
			"mode":{"type":"string","enum":["safe","strict"]}
		}
	}`)

	violations := ValidateArguments(args, schema)
	if len(violations) != 0 {
		t.Fatalf("expected no violations, got %#v", violations)
	}
}

func TestValidateArgumentsNilOrEmptySchema(t *testing.T) {
	t.Parallel()

	args := map[string]any{"name": "x"}
	if violations := ValidateArguments(args, nil); len(violations) != 0 {
		t.Fatalf("expected no violations for nil schema, got %#v", violations)
	}
	if violations := ValidateArguments(args, json.RawMessage(`{}`)); len(violations) != 0 {
		t.Fatalf("expected no violations for empty schema, got %#v", violations)
	}
}

func TestValidateArgumentsInvalidSchema(t *testing.T) {
	t.Parallel()

	violations := ValidateArguments(map[string]any{"name": "x"}, json.RawMessage(`{"invalid"`))
	if len(violations) == 0 {
		t.Fatal("expected violation for invalid schema")
	}
}

func TestMatchesJSONTypeVariants(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		value    any
		expected string
		want     bool
	}{
		{"string ok", "x", "string", true},
		{"boolean ok", true, "boolean", true},
		{"number ok", 1.5, "number", true},
		{"integer ok float", 3.0, "integer", true},
		{"integer fail", 3.2, "integer", false},
		{"object ok", map[string]any{"a": 1}, "object", true},
		{"array ok", []any{1, 2}, "array", true},
		{"unknown type", "x", "custom", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := matchesJSONType(tt.value, tt.expected); got != tt.want {
				t.Fatalf("expected %v, got %v", tt.want, got)
			}
		})
	}
}

func TestNumericValueVariants(t *testing.T) {
	t.Parallel()

	values := []any{
		int(1), int8(1), int16(1), int32(1), int64(1),
		uint(1), uint8(1), uint16(1), uint32(1), uint64(1),
		float32(1.5), float64(2.5), json.Number("3.5"),
	}
	for _, value := range values {
		if _, ok := numericValue(value); !ok {
			t.Fatalf("expected numeric value for %T", value)
		}
	}

	if _, ok := numericValue("not-number"); ok {
		t.Fatal("expected non-numeric string to fail numericValue")
	}
}

func TestValueInEnumNumericCoercion(t *testing.T) {
	t.Parallel()

	enum := []any{1.0, "two"}
	if !valueInEnum(1, enum) {
		t.Fatal("expected integer 1 to match float enum 1.0")
	}
	if !valueInEnum("two", enum) {
		t.Fatal("expected string enum match")
	}
	if valueInEnum("three", enum) {
		t.Fatal("expected value not in enum")
	}
}
