package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveImageFileUsesMimeExtension(t *testing.T) {
	dir := t.TempDir()
	data := []byte("image data")

	if err := saveImageFile(dir, 42, "image/png", data); err != nil {
		t.Fatalf("saveImageFile: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dir, "42.png"))
	if err != nil {
		t.Fatalf("read saved image: %v", err)
	}
	if string(got) != string(data) {
		t.Fatalf("saved image = %q, want %q", got, data)
	}
}

func TestSaveImageFileRejectsUnsupportedMime(t *testing.T) {
	if err := saveImageFile(t.TempDir(), 42, "text/plain", []byte("not an image")); err == nil {
		t.Fatal("saveImageFile accepted unsupported mime type")
	}
}

func TestCleanupGeneratedImages(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"10000.jpg", "10001.png", "not-an-image.tmp"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("image"), 0644); err != nil {
			t.Fatalf("write fixture %q: %v", name, err)
		}
	}

	if err := cleanupGeneratedImages(dir, 10000); err != nil {
		t.Fatalf("cleanupGeneratedImages: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "10000.jpg")); err != nil {
		t.Fatalf("baseline image was removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "10001.png")); !os.IsNotExist(err) {
		t.Fatalf("generated image still exists, stat error = %v", err)
	}
}
