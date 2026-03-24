package search

import (
	"testing"
)

func TestExpandQuery(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    []string
		wantAll  []string // all of these must appear in the result
		wantNone []string // none of these should appear
	}{
		{
			name:    "kubernetes expands to k8s group",
			input:   []string{"kubernetes"},
			wantAll: []string{"kubernetes", "k8s", "kube", "kubectl"},
		},
		{
			name:    "k8s expands to kubernetes group",
			input:   []string{"k8s"},
			wantAll: []string{"kubernetes", "k8s", "kube", "kubectl"},
		},
		{
			name:    "db expands to database",
			input:   []string{"db"},
			wantAll: []string{"db", "database"},
		},
		{
			name:     "no match passes through",
			input:    []string{"foobar"},
			wantAll:  []string{"foobar"},
			wantNone: []string{"kubernetes", "k8s"},
		},
		{
			name:  "empty input",
			input: nil,
		},
		{
			name:    "mixed known and unknown",
			input:   []string{"pods", "kubernetes"},
			wantAll: []string{"pods", "kubernetes", "k8s", "kube", "kubectl"},
		},
		{
			name:    "list expands to ls and enumerate",
			input:   []string{"list"},
			wantAll: []string{"list", "ls", "enumerate"},
		},
		{
			name:    "config expands",
			input:   []string{"config"},
			wantAll: []string{"config", "configuration", "cfg"},
		},
		{
			name:    "duplicate input tokens deduplicated",
			input:   []string{"k8s", "k8s"},
			wantAll: []string{"k8s", "kubernetes", "kube", "kubectl"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ExpandQuery(tt.input)

			if len(tt.input) == 0 {
				if got != nil {
					t.Errorf("ExpandQuery(nil) = %v, want nil", got)
				}
				return
			}

			gotSet := make(map[string]struct{}, len(got))
			for _, g := range got {
				gotSet[g] = struct{}{}
			}

			for _, want := range tt.wantAll {
				if _, ok := gotSet[want]; !ok {
					t.Errorf("ExpandQuery(%v) missing expected token %q; got %v", tt.input, want, got)
				}
			}

			for _, notWant := range tt.wantNone {
				if _, ok := gotSet[notWant]; ok {
					t.Errorf("ExpandQuery(%v) should not contain %q; got %v", tt.input, notWant, got)
				}
			}

			// Verify no duplicates.
			if len(got) != len(gotSet) {
				t.Errorf("ExpandQuery(%v) has duplicates: %v", tt.input, got)
			}
		})
	}
}

func TestExpandQueryWith_CustomProvider(t *testing.T) {
	t.Parallel()

	custom := NewMapSynonymProvider(map[string][]string{
		"car": {"car", "automobile", "vehicle"},
	})

	got := ExpandQueryWith([]string{"car"}, custom)

	wantAll := []string{"car", "automobile", "vehicle"}
	gotSet := make(map[string]struct{}, len(got))
	for _, g := range got {
		gotSet[g] = struct{}{}
	}
	for _, want := range wantAll {
		if _, ok := gotSet[want]; !ok {
			t.Errorf("expected %q in result, got %v", want, got)
		}
	}
}

func TestMergedSynonymProvider(t *testing.T) {
	t.Parallel()

	primary := NewMapSynonymProvider(map[string][]string{
		"car": {"car", "automobile"},
	})
	fallback := NewMapSynonymProvider(map[string][]string{
		"car":  {"car", "vehicle"},          // overridden by primary
		"bike": {"bike", "bicycle", "cycle"}, // only in fallback
	})

	merged := NewMergedSynonymProvider(primary, fallback)

	// "car" should come from primary.
	carGroup := merged.Lookup("car")
	if len(carGroup) != 2 || carGroup[0] != "car" || carGroup[1] != "automobile" {
		t.Errorf("expected primary car group, got %v", carGroup)
	}

	// "bike" should come from fallback.
	bikeGroup := merged.Lookup("bike")
	if len(bikeGroup) != 3 {
		t.Errorf("expected fallback bike group with 3 entries, got %v", bikeGroup)
	}

	// Unknown token returns nil.
	unknownGroup := merged.Lookup("unknown")
	if unknownGroup != nil {
		t.Errorf("expected nil for unknown, got %v", unknownGroup)
	}
}

func TestStaticSynonymProvider(t *testing.T) {
	t.Parallel()

	provider := StaticSynonymProvider{}

	group := provider.Lookup("kubernetes")
	if len(group) == 0 {
		t.Fatal("expected non-empty group for 'kubernetes'")
	}

	found := false
	for _, s := range group {
		if s == "k8s" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected 'k8s' in kubernetes group")
	}

	// Unknown.
	if provider.Lookup("nonexistent") != nil {
		t.Error("expected nil for unknown token")
	}
}

func TestMapSynonymProvider_NilMap(t *testing.T) {
	t.Parallel()

	provider := NewMapSynonymProvider(nil)
	if provider.Lookup("anything") != nil {
		t.Error("expected nil from nil-map provider")
	}
}
