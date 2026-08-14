package sinkpush

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindPPCLI_LocalBinPreferred(t *testing.T) {
	// Create temp home with .local/bin and go/bin, both containing the binary.
	// .local/bin should win because it's checked first.
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("PATH", "") // Stripped PATH

	localBin := filepath.Join(tmpHome, ".local", "bin")
	goBin := filepath.Join(tmpHome, "go", "bin")
	if err := os.MkdirAll(localBin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(goBin, 0o755); err != nil {
		t.Fatal(err)
	}

	localPath := filepath.Join(localBin, "instacart-pp-cli")
	goPath := filepath.Join(goBin, "instacart-pp-cli")
	if err := os.WriteFile(localPath, []byte("#!/bin/bash\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(goPath, []byte("#!/bin/bash\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	got := findPPCLI("instacart-pp-cli")
	if got != localPath {
		t.Errorf("findPPCLI with both .local/bin and go/bin: got %q, want %q", got, localPath)
	}
}

func TestFindPPCLI_OnlyLocalBin_StrippedPATH(t *testing.T) {
	// Binary ONLY at .local/bin/instacart-pp-cli, PATH without that dir.
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("PATH", "/nonexistent")

	localBin := filepath.Join(tmpHome, ".local", "bin")
	if err := os.MkdirAll(localBin, 0o755); err != nil {
		t.Fatal(err)
	}
	localPath := filepath.Join(localBin, "instacart-pp-cli")
	if err := os.WriteFile(localPath, []byte("#!/bin/bash\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	got := findPPCLI("instacart-pp-cli")
	if got != localPath {
		t.Errorf("findPPCLI with only .local/bin (stripped PATH): got %q, want %q", got, localPath)
	}
}

func TestFindPPCLI_OnlyGoBin_StillWorks(t *testing.T) {
	// Only ~/go/bin -> still works.
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("PATH", "")

	goBin := filepath.Join(tmpHome, "go", "bin")
	if err := os.MkdirAll(goBin, 0o755); err != nil {
		t.Fatal(err)
	}
	goPath := filepath.Join(goBin, "instacart-pp-cli")
	if err := os.WriteFile(goPath, []byte("#!/bin/bash\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	got := findPPCLI("instacart-pp-cli")
	if got != goPath {
		t.Errorf("findPPCLI with only go/bin: got %q, want %q", got, goPath)
	}
}

func TestFindPPCLI_AliasOnPATH(t *testing.T) {
	// Alias instacart on PATH when instacart-pp-cli missing -> Instacart installed.
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	// Create a temp bin dir with the alias and add to PATH.
	tmpBin := t.TempDir()
	aliasPath := filepath.Join(tmpBin, "instacart")
	if err := os.WriteFile(aliasPath, []byte("#!/bin/bash\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", tmpBin)

	got := findPPCLI("instacart-pp-cli", "instacart")
	if got != aliasPath {
		t.Errorf("findPPCLI with alias on PATH: got %q, want %q", got, aliasPath)
	}
}

func TestFindPPCLI_NothingAnywhere_ReturnsDefault(t *testing.T) {
	// Nothing anywhere -> returns default path (so error messages reference it).
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("PATH", "")

	got := findPPCLI("instacart-pp-cli", "instacart")
	// Should return the preferred path for error messages.
	expected := filepath.Join(tmpHome, ".local", "bin", "instacart-pp-cli")
	if got != expected {
		t.Errorf("findPPCLI with nothing installed: got %q, want %q", got, expected)
	}
}

func TestFindPPCLI_UserBin(t *testing.T) {
	// Only ~/bin -> found.
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("PATH", "")

	userBin := filepath.Join(tmpHome, "bin")
	if err := os.MkdirAll(userBin, 0o755); err != nil {
		t.Fatal(err)
	}
	binPath := filepath.Join(userBin, "airbnb-pp-cli")
	if err := os.WriteFile(binPath, []byte("#!/bin/bash\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	got := findPPCLI("airbnb-pp-cli")
	if got != binPath {
		t.Errorf("findPPCLI with ~/bin: got %q, want %q", got, binPath)
	}
}

func TestFindPPCLI_DirectoryNotFile_Skipped(t *testing.T) {
	// A directory at the expected path should not be considered a valid binary.
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("PATH", "")

	localBin := filepath.Join(tmpHome, ".local", "bin")
	if err := os.MkdirAll(localBin, 0o755); err != nil {
		t.Fatal(err)
	}
	// Create a directory instead of a file.
	dirPath := filepath.Join(localBin, "instacart-pp-cli")
	if err := os.MkdirAll(dirPath, 0o755); err != nil {
		t.Fatal(err)
	}

	// But put a real file in go/bin.
	goBin := filepath.Join(tmpHome, "go", "bin")
	if err := os.MkdirAll(goBin, 0o755); err != nil {
		t.Fatal(err)
	}
	goPath := filepath.Join(goBin, "instacart-pp-cli")
	if err := os.WriteFile(goPath, []byte("#!/bin/bash\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	got := findPPCLI("instacart-pp-cli")
	if got != goPath {
		t.Errorf("findPPCLI should skip directory and find go/bin: got %q, want %q", got, goPath)
	}
}

func TestFindPPCLI_PycookiecheatAdapters_LocalBin(t *testing.T) {
	// Test that pycookiecheat-style adapters (airbnb, ebay, pagliacci) can
	// also find their CLI at .local/bin.
	for _, name := range []string{"airbnb-pp-cli", "ebay-pp-cli", "pagliacci-pp-cli"} {
		t.Run(name, func(t *testing.T) {
			tmpHome := t.TempDir()
			t.Setenv("HOME", tmpHome)
			t.Setenv("PATH", "")

			localBin := filepath.Join(tmpHome, ".local", "bin")
			if err := os.MkdirAll(localBin, 0o755); err != nil {
				t.Fatal(err)
			}
			localPath := filepath.Join(localBin, name)
			if err := os.WriteFile(localPath, []byte("#!/bin/bash\nexit 0\n"), 0o755); err != nil {
				t.Fatal(err)
			}

			got := findPPCLI(name)
			if got != localPath {
				t.Errorf("findPPCLI(%q) with .local/bin: got %q, want %q", name, got, localPath)
			}
		})
	}
}

func TestFindPPCLI_TableReservation_LocalBin(t *testing.T) {
	// Test table-reservation-goat-pp-cli at .local/bin.
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("PATH", "")

	localBin := filepath.Join(tmpHome, ".local", "bin")
	if err := os.MkdirAll(localBin, 0o755); err != nil {
		t.Fatal(err)
	}
	localPath := filepath.Join(localBin, "table-reservation-goat-pp-cli")
	if err := os.WriteFile(localPath, []byte("#!/bin/bash\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	got := findPPCLI("table-reservation-goat-pp-cli")
	if got != localPath {
		t.Errorf("findPPCLI(table-reservation) with .local/bin: got %q, want %q", got, localPath)
	}
}
