package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestQQImageContainerKeyMapping(t *testing.T) {
	for _, name := range []string{"docker-compose.yml", "docker-compose.local.yml", "docker-compose.standalone.yml", "docker-compose.dev.yml"} {
		data, err := os.ReadFile(filepath.Join("..", "..", "..", "deploy", name))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), "QQ_IMAGE_ENCRYPTION_KEY=${QQ_IMAGE_ENCRYPTION_KEY:-}") {
			t.Errorf("%s must pass the optional QQ image encryption key", name)
		}
	}
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "deploy", ".env.example"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "QQ_IMAGE_ENCRYPTION_KEY=") {
		t.Error("example must document an empty key, not generate secrets")
	}
}
