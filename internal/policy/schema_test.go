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
