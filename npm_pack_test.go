package workloom

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestNPMPackagesDoNotDownloadAndExposeOneCommand(t *testing.T) {
	root := repoRoot(t)
	wrapper := readPackageJSON(t, filepath.Join(root, "npm", "workloom", "package.json"))
	if wrapper["name"] != "@jayy513/workloom" {
		t.Fatalf("wrapper name = %v", wrapper["name"])
	}
	bin, _ := wrapper["bin"].(map[string]any)
	if len(bin) != 1 || bin["workloom"] == nil {
		t.Fatalf("bin = %v, want only workloom", bin)
	}
	if scripts, ok := wrapper["scripts"].(map[string]any); ok && scripts["postinstall"] != nil {
		t.Fatal("wrapper has a postinstall script")
	}
	opts, _ := wrapper["optionalDependencies"].(map[string]any)
	if len(opts) != 6 {
		t.Fatalf("optionalDependencies = %d, want 6", len(opts))
	}
	for name := range opts {
		rel := strings.TrimPrefix(name, "@jayy513/workloom-")
		pkg := readPackageJSON(t, filepath.Join(root, "npm", "platforms", rel, "package.json"))
		if pkg["name"] != name {
			t.Errorf("%s name = %v", rel, pkg["name"])
		}
		if _, ok := pkg["bin"]; ok {
			t.Errorf("%s exposes a bin; platform packages must not add a second command", rel)
		}
	}
}

func TestPackNPMEmbedsReleaseBinaries(t *testing.T) {
	bashExe := gitBash(t)
	nodeExe, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed")
	}
	if _, err := exec.LookPath("npm"); err != nil {
		t.Skip("npm not installed")
	}
	nodeDir := filepath.ToSlash(filepath.Dir(nodeExe))
	if len(nodeDir) >= 2 && nodeDir[1] == ':' {
		nodeDir = "/" + strings.ToLower(nodeDir[:1]) + nodeDir[2:]
	}
	root := repoRoot(t)
	dist := t.TempDir()
	payload := []byte("fixture-binary-not-a-download")
	for _, name := range []string{
		"workloom-darwin-arm64",
		"workloom-darwin-amd64",
		"workloom-linux-arm64",
		"workloom-linux-amd64",
		"workloom-windows-arm64.exe",
		"workloom-windows-amd64.exe",
	} {
		if err := os.WriteFile(filepath.Join(dist, name), payload, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	out := t.TempDir()
	cmd := exec.Command(bashExe, "scripts/pack-npm.sh", "--version", "v0.0.1", "--dist", filepath.ToSlash(dist), "--out", filepath.ToSlash(out))
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "PATH="+nodeDir+":/usr/bin:/bin")
	if raw, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("pack-npm: %v\n%s", err, raw)
	}
	tarball := filepath.Join(out, "jayy513-workloom-win32-x64-0.0.1.tgz")
	body := readTarballFile(t, tarball, "package/workloom.exe")
	if !bytes.Equal(body, payload) {
		t.Fatalf("packed binary = %q, want the release fixture", body)
	}
	manifest := readTarballFile(t, tarball, "package/package.json")
	var doc map[string]any
	if err := json.Unmarshal(manifest, &doc); err != nil {
		t.Fatal(err)
	}
	if doc["version"] != "0.0.1" {
		t.Fatalf("packed version = %v", doc["version"])
	}
	if _, ok := doc["bin"]; ok {
		t.Fatal("packed platform package exposes a bin")
	}
	if scripts, ok := doc["scripts"].(map[string]any); ok && scripts["postinstall"] != nil {
		t.Fatal("packed platform package has postinstall")
	}
	wrapperTar := filepath.Join(out, "jayy513-workloom-0.0.1.tgz")
	wrapperManifest := readTarballFile(t, wrapperTar, "package/package.json")
	var wrapper map[string]any
	if err := json.Unmarshal(wrapperManifest, &wrapper); err != nil {
		t.Fatal(err)
	}
	opts, _ := wrapper["optionalDependencies"].(map[string]any)
	if opts["@jayy513/workloom-win32-x64"] != "0.0.1" {
		t.Fatalf("wrapper optionalDependency version = %v", opts["@jayy513/workloom-win32-x64"])
	}
	bin, _ := wrapper["bin"].(map[string]any)
	if len(bin) != 1 || bin["workloom"] == nil {
		t.Fatalf("packed wrapper bin = %v", bin)
	}
}

func TestNPMWrapperExecsPlantedBinaryAndRefusesToDownload(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed")
	}
	raw, err := exec.Command(node, "-p", "process.platform + '\\n' + process.arch + '\\n' + process.execPath").Output()
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(parts) != 3 {
		t.Fatalf("node identity = %q", raw)
	}
	platform, arch, execPath := parts[0], parts[1], parts[2]
	root := repoRoot(t)
	shim := filepath.Join(root, "npm", "workloom", "bin", "workloom.js")

	missingDir := t.TempDir()
	missingShim := filepath.Join(missingDir, "workloom.js")
	if err := copyFile(shim, missingShim); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, missingShim, "--version")
	cmd.Dir = missingDir
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("missing platform package exited 0: %s", out)
	}
	if !strings.Contains(string(out), "does not download") {
		t.Fatalf("stderr = %s, want a refusal that does not download", out)
	}

	home := t.TempDir()
	pkgName := "@jayy513/workloom-" + platform + "-" + arch
	pkgDir := filepath.Join(home, "node_modules", "@jayy513", "workloom-"+platform+"-"+arch)
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	meta := map[string]any{"name": pkgName, "version": "0.0.1"}
	encoded, _ := json.Marshal(meta)
	if err := os.WriteFile(filepath.Join(pkgDir, "package.json"), encoded, 0o644); err != nil {
		t.Fatal(err)
	}
	binName := "workloom"
	if platform == "win32" {
		binName = "workloom.exe"
	}
	if err := copyFile(execPath, filepath.Join(pkgDir, binName)); err != nil {
		t.Fatal(err)
	}
	planted := filepath.Join(home, "workloom.js")
	if err := copyFile(shim, planted); err != nil {
		t.Fatal(err)
	}
	run := exec.Command(node, planted, "-e", "process.exit(7)")
	run.Dir = home
	if err := run.Run(); err == nil {
		t.Fatal("planted binary did not receive the forwarded exit code")
	} else if exitErr, ok := err.(*exec.ExitError); !ok || exitErr.ExitCode() != 7 {
		t.Fatalf("exit = %v, want 7", err)
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return wd
}

func readPackageJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return doc
}

func readTarballFile(t *testing.T, tarball, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(tarball)
	if err != nil {
		t.Fatal(err)
	}
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err != nil {
			t.Fatalf("%s missing %s: %v", tarball, name, err)
		}
		if hdr.Name == name {
			var buf bytes.Buffer
			if _, err := buf.ReadFrom(tr); err != nil {
				t.Fatal(err)
			}
			return buf.Bytes()
		}
	}
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o755)
}

func gitBash(t *testing.T) string {
	t.Helper()
	for _, candidate := range []string{
		`C:\Program Files\Git\bin\bash.exe`,
		`C:\Program Files\Git\usr\bin\bash.exe`,
	} {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	if p, err := exec.LookPath("bash"); err == nil {
		return p
	}
	t.Skip("bash not installed")
	return ""
}
