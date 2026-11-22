package fff

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func serve(t *testing.T, body string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/"+Version+"/fff-mcp-test" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	oldReleases, oldBuilds := releases, builds
	t.Cleanup(func() { releases, builds = oldReleases, oldBuilds })
	sum := sha256.Sum256([]byte("the real binary"))
	releases = srv.URL + "/"
	builds = map[string]struct{ name, sha256 string }{"test/os": {"fff-mcp-test", hex.EncodeToString(sum[:])}}
}

func TestDownloadChecksTheSum(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "bin", "fff-mcp")

	serve(t, "the real binary")
	if err := download(context.Background(), "test/os", dest); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(dest)
	if err != nil || info.Mode().Perm() != 0o755 {
		t.Fatalf("installed: %v %v", info, err)
	}

	tampered := filepath.Join(t.TempDir(), "fff-mcp")
	serve(t, "something else")
	if err := download(context.Background(), "test/os", tampered); err == nil {
		t.Fatal("a download with the wrong sum was accepted")
	}
	if entries, _ := os.ReadDir(filepath.Dir(tampered)); len(entries) != 0 {
		t.Fatalf("left behind: %v", entries)
	}

	if err := download(context.Background(), "plan9/mips", tampered); err == nil {
		t.Fatal("an unknown platform was accepted")
	}
}

func TestEveryPinnedBuildHasASum(t *testing.T) {
	for platform, b := range builds {
		if len(b.sha256) != 64 || b.name == "" {
			t.Errorf("%s: %+v", platform, b)
		}
	}
}
