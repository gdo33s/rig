package fff

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// Version is the fff-mcp release rig installs. Its checksums are pinned
// below, so a download that differs from what was reviewed is refused.
const Version = "v0.11.0"

// releases is where fff publishes its binaries.
var releases = "https://github.com/dmtrKovalenko/fff/releases/download/"

// builds are Version's binaries for each platform rig runs on, by GOOS/GOARCH,
// with their SHA-256 sums from the release. Linux uses the static musl
// build, which runs on any distribution.
var builds = map[string]struct{ name, sha256 string }{
	"darwin/arm64": {"fff-mcp-aarch64-apple-darwin", "cc606d9418cfa7764376dcd7b32a33e6459a6cb9a4718be366b024dca6e9fead"},
	"darwin/amd64": {"fff-mcp-x86_64-apple-darwin", "33377a8f96442ebc8b06d3bd2863cf66a520f97a33700e0fb85a09dd7988e862"},
	"linux/arm64":  {"fff-mcp-aarch64-unknown-linux-musl", "35f8e05d6c865266bf451d13bfa5e1b0d98438fda7dc1c9bdbc1c6a83484577d"},
	"linux/amd64":  {"fff-mcp-x86_64-unknown-linux-musl", "d1e8671f40c89c445aaa1728a21ec04b74be2e5f99091485caeabcc97f99aceb"},
}

// Ensure makes sure fff-mcp is installed. When it is not, Ensure downloads
// Version into ~/.local/bin, where Binary finds it. It reports the path,
// and whether it had to download it.
func Ensure(ctx context.Context) (string, bool, error) {
	if p, err := Binary(); err == nil {
		return p, false, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", false, err
	}
	dest := filepath.Join(home, ".local", "bin", "fff-mcp")
	if err := download(ctx, runtime.GOOS+"/"+runtime.GOARCH, dest); err != nil {
		return "", false, err
	}
	return dest, true, nil
}

// download fetches the build for platform to dest, checks its sum, and
// only then puts it in place, so a failed or tampered download leaves
// nothing behind.
func download(ctx context.Context, platform, dest string) error {
	b, ok := builds[platform]
	if !ok {
		return fmt.Errorf("fff-mcp has no build for %s", platform)
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, releases+Version+"/"+b.name, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("downloading fff-mcp: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("downloading fff-mcp: %s", resp.Status)
	}

	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dest), ".fff-mcp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	sum := sha256.New()
	_, err = io.Copy(io.MultiWriter(tmp, sum), resp.Body)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return fmt.Errorf("downloading fff-mcp: %w", err)
	}
	if got := hex.EncodeToString(sum.Sum(nil)); got != b.sha256 {
		return fmt.Errorf("fff-mcp download has the wrong checksum (%s), so it was not installed", got[:12])
	}
	if err := os.Chmod(tmp.Name(), 0o755); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), dest)
}
