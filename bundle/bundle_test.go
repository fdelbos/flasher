package bundle

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	dir := t.TempDir()
	app := []byte("hello firmware")
	if err := os.WriteFile(filepath.Join(dir, "app.bin"), app, 0o644); err != nil {
		t.Fatal(err)
	}
	args := `{"flash_settings":{"flash_mode":"dio","flash_size":"4MB","flash_freq":"80m"},
	          "flash_files":{"0x10000":"app.bin"},
	          "extra_esptool_args":{"chip":"esp32c6"}}`
	if err := os.WriteFile(filepath.Join(dir, "flasher_args.json"), []byte(args), 0o644); err != nil {
		t.Fatal(err)
	}

	b, err := FromBuildDir(dir, "v1", "now")
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := b.Pack(&buf); err != nil {
		t.Fatal(err)
	}

	got, err := Open(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if got.Manifest.Chip != "esp32c6" || got.Manifest.Version != "v1" {
		t.Fatalf("manifest = %+v", got.Manifest)
	}
	ff := got.FlashFiles()
	if len(ff) != 1 || ff[0].Offset != 0x10000 || !bytes.Equal(ff[0].Data, app) {
		t.Fatalf("flash files = %+v", ff)
	}
	if got.Manifest.Files[0].Role != "app" {
		t.Fatalf("role = %s", got.Manifest.Files[0].Role)
	}
}

func TestOpenRejectsCorruptedFile(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "app.bin"), []byte("data"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "flasher_args.json"),
		[]byte(`{"flash_files":{"0x0":"app.bin"},"extra_esptool_args":{"chip":"esp32c6"}}`), 0o644)
	b, _ := FromBuildDir(dir, "", "")
	b.data["app.bin"] = []byte("tampered") // break the sha without updating the manifest
	var buf bytes.Buffer
	_ = b.Pack(&buf)
	if _, err := Open(&buf); err == nil {
		t.Fatal("expected sha256 mismatch error")
	}
}
