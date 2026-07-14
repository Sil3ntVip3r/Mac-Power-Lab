package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestMacSecurityScriptsAreProjectScoped(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix paths")
	}
	root := filepath.Clean(filepath.Join("..", ".."))
	paths := []string{
		filepath.Join(root, "scripts", "prepare_macos_security.sh"),
		filepath.Join(root, "scripts", "build_macos.sh"),
		filepath.Join(root, "scripts", "build_swiftui_app.sh"),
	}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		text := string(data)
		if strings.Contains(text, "spctl --master-disable") || strings.Contains(text, "spctl --global-disable") {
			t.Fatalf("%s disables Gatekeeper globally", path)
		}
	}
}

func TestBuildMacOSHasExecutionSmokeTest(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", ".."))
	data, err := os.ReadFile(filepath.Join(root, "scripts", "build_macos.sh"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, required := range []string{"codesign --force --sign -", "./bin/macpowerlab version", "com.apple.quarantine"} {
		if !strings.Contains(text, required) {
			t.Fatalf("build_macos.sh missing %q", required)
		}
	}
}
