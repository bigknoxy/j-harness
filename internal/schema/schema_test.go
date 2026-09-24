package schema

import (
	"os"
	"path/filepath"
	"testing"
)

func mustCompile(t *testing.T, raw string) *Schema {
	t.Helper()
	s, err := Compile([]byte(raw))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	return s
}

func TestCompileRejectsBadSchema(t *testing.T) {
	cases := map[string]string{
		"not object":     `[]`,
		"bad json":       `{`,
		"unknown type":   `{"type":"sttring"}`,
		"bad type field": `{"type":5}`,
		"bad property":   `{"properties":{"a":5}}`,
	}
	for name, raw := range cases {
		if _, err := Compile([]byte(raw)); err == nil {
			t.Errorf("%s: expected error, got nil", name)
		}
	}
}

func TestValidateTriageSchema(t *testing.T) {
	s := mustCompile(t, `{
		"type":"object",
		"required":["category","priority"],
		"additionalProperties":false,
		"properties":{
			"category":{"type":"string","enum":["billing","technical","other"]},
			"priority":{"type":"string","enum":["low","normal","high"]}
		}
	}`)

	if err := s.Validate(`{"category":"billing","priority":"high"}`); err != nil {
		t.Fatalf("valid doc rejected: %v", err)
	}

	fails := map[string]string{
		"missing priority": `{"category":"billing"}`,
		"bad enum":         `{"category":"sales","priority":"high"}`,
		"extra property":   `{"category":"billing","priority":"high","note":"x"}`,
		"wrong type":       `{"category":5,"priority":"high"}`,
		"not json":         `category=billing`,
		"not object":       `"billing"`,
	}
	for name, doc := range fails {
		if err := s.Validate(doc); err == nil {
			t.Errorf("%s: expected error, got nil", name)
		}
	}
}

func TestValidateNumberAndString(t *testing.T) {
	s := mustCompile(t, `{
		"type":"object",
		"properties":{
			"score":{"type":"number","minimum":0,"maximum":10},
			"name":{"type":"string","minLength":2,"maxLength":4}
		}
	}`)
	if err := s.Validate(`{"score":7,"name":"bob"}`); err != nil {
		t.Fatalf("valid doc rejected: %v", err)
	}
	for _, doc := range []string{`{"score":11}`, `{"score":-1}`, `{"name":"a"}`, `{"name":"abcde"}`} {
		if err := s.Validate(doc); err == nil {
			t.Errorf("%s: expected error, got nil", doc)
		}
	}
}

func TestValidateArrayAndInteger(t *testing.T) {
	s := mustCompile(t, `{
		"type":"array",
		"minItems":1,
		"maxItems":2,
		"items":{"type":"integer"}
	}`)
	if err := s.Validate(`[1,2]`); err != nil {
		t.Fatalf("valid doc rejected: %v", err)
	}
	for _, doc := range []string{`[]`, `[1,2,3]`, `[1.5]`, `["x"]`} {
		if err := s.Validate(doc); err == nil {
			t.Errorf("%s: expected error, got nil", doc)
		}
	}
}

func TestValidateTypeUnion(t *testing.T) {
	s := mustCompile(t, `{"type":["string","null"]}`)
	if err := s.Validate(`"hi"`); err != nil {
		t.Fatalf("string rejected: %v", err)
	}
	if err := s.Validate(`null`); err != nil {
		t.Fatalf("null rejected: %v", err)
	}
	if err := s.Validate(`5`); err == nil {
		t.Error("number accepted, want error")
	}
}

func TestCompileTriageFile(t *testing.T) {
	path := filepath.Join("..", "..", "agent-registry", "schemas", "triage.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read schema: %v", err)
	}
	s, err := Compile(raw)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if err := s.Validate(`{"category":"technical","priority":"normal"}`); err != nil {
		t.Fatalf("valid doc rejected: %v", err)
	}
}
