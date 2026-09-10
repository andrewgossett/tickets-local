package main

import (
	"archive/zip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSourceArchiveExcludesOperationalData(t *testing.T) {
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("Git is required for source archive validation")
	}
	attributes, err := os.ReadFile(".gitattributes")
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	runGit := func(args ...string) string {
		t.Helper()
		command := exec.Command(git, args...)
		command.Dir = directory
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v failed: %v: %s", args, err, output)
		}
		return strings.TrimSpace(string(output))
	}
	runGit("init", "--quiet")
	if err := os.WriteFile(filepath.Join(directory, ".gitattributes"), attributes, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, prefix := range []string{"", "nested/"} {
		for _, name := range []string{
			"dist/build.exe", ".tools/tool.exe", ".pnpm-home/cache", "logs/output.txt",
			"attachments/photo.png", "snapshots/backup.ndjson", "events.ndjson", "events.ndjson.replacing",
			"network.json", "aprs-credentials.json", "aprs-passcode", "integration-cache.json",
			"application.log", "signing.key", "signing.pem", "signing.p12", "signing.pfx", "id_rsa", "id_ed25519",
		} {
			path := filepath.Join(directory, filepath.FromSlash(prefix+name))
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte("synthetic packaging test fixture\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := os.WriteFile(filepath.Join(directory, "main.go"), []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit("add", "--force", ".")
	tree := runGit("write-tree")
	// A source archive must not pick up untracked files of any name either.
	if err := os.WriteFile(filepath.Join(directory, "private-notes.txt"), []byte("synthetic untracked fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	archivePath := filepath.Join(t.TempDir(), "source.zip")
	runGit("archive", "--format=zip", "--output="+archivePath, tree)
	archive, err := zip.OpenReader(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	foundSource := false
	for _, entry := range archive.File {
		if entry.FileInfo().IsDir() {
			continue
		}
		switch entry.Name {
		case "main.go":
			foundSource = true
		case ".gitattributes":
		default:
			t.Errorf("private or operational path entered source archive: %s", entry.Name)
		}
	}
	if !foundSource {
		t.Fatal("source missing from archive")
	}
}

func TestMacReleaseCarriesPinnedThirdPartyComplianceFiles(t *testing.T) {
	kitScript, err := os.ReadFile("scripts/package-macos-signing-kit.sh")
	if err != nil {
		t.Fatal(err)
	}
	signingScript, err := os.ReadFile("packaging/macos/Sign and Notarize Tickets Local.command")
	if err != nil {
		t.Fatal(err)
	}
	kit := string(kitScript)
	signing := string(signingScript)
	for _, required := range []string{
		"DireWolf-${direwolf_version}-source.tar.gz",
		"gpsd-3.27.5-source.tar.xz",
		"hamlib-4.7.2-source.tar.gz",
		"hidapi-0.15.0-source.tar.gz",
		"portaudio-19.7.0-source.tgz",
		"libusb-1.0.30-source.tar.bz2",
	} {
		if !strings.Contains(kit, required) {
			t.Errorf("macOS signing kit is missing pinned source %q", required)
		}
	}
	for _, required := range []string{"TICKETS_LOCAL_SOURCE_CACHE", "--retry 3", "expected_sha256"} {
		if !strings.Contains(kit, required) {
			t.Errorf("macOS signing kit is missing resilient source-download behavior %q", required)
		}
	}
	for _, required := range []string{"THIRD-PARTY-NOTICES.md", "Third-Party-Source", "-eq 6"} {
		if !strings.Contains(signing, required) {
			t.Errorf("macOS signing workflow is missing compliance guard %q", required)
		}
	}
}
