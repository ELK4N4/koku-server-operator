package resources

import (
	"testing"
)

func TestMergeEnvStableOrder(t *testing.T) {
	overrides := map[string]string{
		"Z_LAST":  "z",
		"A_FIRST": "a",
		"M_MID":   "m",
	}
	var first []string
	for i := 0; i < 20; i++ {
		merged := MergeEnv(nil, overrides)
		names := make([]string, len(merged))
		for j, e := range merged {
			names[j] = e.Name
		}
		if i == 0 {
			first = names
			continue
		}
		for j := range names {
			if names[j] != first[j] {
				t.Fatalf("unstable env order on iteration %d: got %v want %v", i, names, first)
			}
		}
	}
	want := []string{"A_FIRST", "M_MID", "Z_LAST"}
	for i, name := range want {
		if first[i] != name {
			t.Fatalf("expected sorted keys %v, got %v", want, first)
		}
	}
}
