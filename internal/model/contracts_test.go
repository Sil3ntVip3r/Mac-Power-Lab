package model

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestContractManifest(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(file), "..", "..")
	raw, err := os.ReadFile(filepath.Join(root, "contracts", "v1", "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Contracts []struct{ File, Schema string } `json:"contracts"`
	}
	if err = json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	if len(manifest.Contracts) != 9 {
		t.Fatalf("contracts=%d", len(manifest.Contracts))
	}
	seen := map[string]bool{}
	for _, c := range manifest.Contracts {
		if seen[c.Schema] {
			t.Fatalf("duplicate %s", c.Schema)
		}
		seen[c.Schema] = true
		b, err := os.ReadFile(filepath.Join(root, "contracts", "v1", c.File))
		if err != nil {
			t.Fatal(err)
		}
		var schema map[string]any
		if json.Unmarshal(b, &schema) != nil {
			t.Fatalf("invalid schema %s", c.File)
		}
		props := schema["properties"].(map[string]any)
		constVal := props["schema"].(map[string]any)["const"]
		if constVal != c.Schema {
			t.Fatalf("%s const=%v", c.File, constVal)
		}
	}
}
