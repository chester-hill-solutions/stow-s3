package stow_test

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestWasmExcludesNativeDependencies(t *testing.T) {
	cmd := exec.Command("go", "list", "-deps", "../../cmd/stow-wasm")
	cmd.Env = append(os.Environ(), "GOOS=js", "GOARCH=wasm")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("list WASM dependencies: %v\n%s", err, output)
	}
	for _, dependency := range strings.Fields(string(output)) {
		if dependency == "net" || dependency == "net/http" || dependency == "crypto/tls" ||
			strings.HasSuffix(dependency, "/pkg/stow") || strings.HasSuffix(dependency, "/internal/storage/workspace") ||
			strings.HasSuffix(dependency, "/internal/storage/fs") || strings.HasSuffix(dependency, "/internal/s3api") ||
			strings.HasSuffix(dependency, "/internal/runthrough") || strings.HasPrefix(dependency, "github.com/aws/") ||
			strings.HasPrefix(dependency, "google.golang.org/protobuf/") {
			t.Errorf("WASM includes native dependency %q", dependency)
		}
	}
}
