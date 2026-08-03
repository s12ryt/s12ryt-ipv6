package webui

import (
	"io/fs"
	"testing"
)

func TestEmbeddedDistributionContainsApplicationShellAndAssets(t *testing.T) {
	index, err := fs.ReadFile(Dist, "index.html")
	if err != nil {
		t.Fatalf("read embedded index: %v", err)
	}
	if len(index) == 0 {
		t.Fatal("embedded index is empty")
	}
	assets, err := fs.ReadDir(Dist, "assets")
	if err != nil {
		t.Fatalf("read embedded assets: %v", err)
	}
	if len(assets) == 0 {
		t.Fatal("embedded assets directory is empty")
	}
}
