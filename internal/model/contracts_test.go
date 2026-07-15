package model

import (
	"bytes"
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
	mirrorManifest, err := os.ReadFile(filepath.Join(root, "schemas", "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw, mirrorManifest) {
		t.Fatal("schema manifests differ")
	}
	var manifest struct {
		Contracts []struct{ File, Schema string } `json:"contracts"`
	}
	if err = json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	if len(manifest.Contracts) != 10 {
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
		mirror, err := os.ReadFile(filepath.Join(root, "schemas", c.File))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(b, mirror) {
			t.Fatalf("schema mirror differs for %s", c.File)
		}
	}
}

func TestSessionSchemaIncludesEffectiveCollectionOptions(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(file), "..", "..")
	raw, err := os.ReadFile(filepath.Join(root, "contracts", "v1", "session.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	var schema map[string]any
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatal(err)
	}
	properties := schema["properties"].(map[string]any)
	options, ok := properties["effective_options"].(map[string]any)
	if !ok {
		t.Fatal("session schema missing effective_options")
	}
	optionProperties := options["properties"].(map[string]any)
	for _, key := range []string{"app_attribution", "top_apps", "sqlite_mirror", "safe_mode"} {
		if _, ok := optionProperties[key]; !ok {
			t.Fatalf("effective_options missing %q", key)
		}
	}
}
