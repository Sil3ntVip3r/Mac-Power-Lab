package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSwiftSourcesAvoidUnavailableLocalizedSortProperty(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", ".."))
	path := filepath.Join(
		root,
		"swiftui",
		"Sources",
		"MacPowerLabApp",
		"Models.swift",
	)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "localizedStandardLowercased") {
		t.Fatal("Models.swift uses unavailable localizedStandardLowercased")
	}
}

func TestZshErrorTrapsAreSafeWithNoUnset(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", ".."))
	for _, relative := range []string{
		"scripts/build_macos.sh",
		"scripts/build_native.sh",
		"scripts/build_swiftui_app.sh",
	} {
		data, err := os.ReadFile(filepath.Join(root, relative))
		if err != nil {
			t.Fatal(err)
		}
		text := string(data)
		if strings.Contains(text, "$ZSH_DEBUG_CMD") &&
			!strings.Contains(text, "${ZSH_DEBUG_CMD:-") {
			t.Fatalf("%s references ZSH_DEBUG_CMD unsafely", relative)
		}
		if strings.Contains(text, "trap 'status=") || strings.Contains(text, "local status=") {
			t.Fatalf("%s assigns zsh's read-only status variable", relative)
		}
	}
}
