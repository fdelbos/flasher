// Package bundle is a portable, self-describing flash archive: a gzip-compressed
// tar holding a manifest plus the firmware images. It lets you build on one
// machine and `flasher flash bundle.fbundle` on any other, with no esp-idf. The
// manifest carries per-file roles, a version, and a signature slot, so the same
// artifact also serves as an OTA payload.
package bundle

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"

	"github.com/fdelbos/flasher/partition"
)

const schemaVersion = 1

// File is one image in the bundle.
type File struct {
	Role   string `json:"role"` // bootloader|partition-table|otadata|nvs|app
	Name   string `json:"name"`
	Offset uint32 `json:"offset"`
	Size   int    `json:"size"`
	SHA256 string `json:"sha256"`
}

// Manifest describes a bundle's contents and flash settings.
type Manifest struct {
	Schema    int    `json:"schema"`
	Chip      string `json:"chip"`
	FlashMode string `json:"flash_mode"`
	FlashSize string `json:"flash_size"`
	FlashFreq string `json:"flash_freq"`
	Version   string `json:"version,omitempty"`
	CreatedAt string `json:"created_at,omitempty"`
	Files     []File `json:"files"`
	Signature string `json:"signature,omitempty"` // reserved for signed OTA
}

// Bundle is a manifest plus the file contents.
type Bundle struct {
	Manifest Manifest
	data     map[string][]byte
}

// FromBuildDir builds a Bundle from an esp-idf build dir (its flasher_args.json).
func FromBuildDir(buildDir, version, createdAt string) (*Bundle, error) {
	fa, files, err := partition.Load(buildDir)
	if err != nil {
		return nil, err
	}
	b := &Bundle{data: map[string][]byte{}, Manifest: Manifest{
		Schema:    schemaVersion,
		Chip:      fa.Extra.Chip,
		FlashMode: fa.FlashSettings.FlashMode,
		FlashSize: fa.FlashSettings.FlashSize,
		FlashFreq: fa.FlashSettings.FlashFreq,
		Version:   version,
		CreatedAt: createdAt,
	}}
	for _, f := range files {
		name := filepath.Base(f.Name)
		sum := sha256.Sum256(f.Data)
		b.Manifest.Files = append(b.Manifest.Files, File{
			Role:   roleOf(name),
			Name:   name,
			Offset: f.Offset,
			Size:   len(f.Data),
			SHA256: hex.EncodeToString(sum[:]),
		})
		b.data[name] = f.Data
	}
	return b, nil
}

func roleOf(name string) string {
	n := strings.ToLower(name)
	switch {
	case strings.Contains(n, "bootloader"):
		return "bootloader"
	case strings.Contains(n, "partition-table"), strings.Contains(n, "partition_table"):
		return "partition-table"
	case strings.Contains(n, "ota_data"), strings.Contains(n, "otadata"):
		return "otadata"
	case strings.Contains(n, "nvs"):
		return "nvs"
	default:
		return "app"
	}
}

// Pack writes the bundle as a gzip-compressed tar.
func (b *Bundle) Pack(w io.Writer) error {
	gz := gzip.NewWriter(w)
	tw := tar.NewWriter(gz)
	mj, err := json.MarshalIndent(b.Manifest, "", "  ")
	if err != nil {
		return err
	}
	if err := writeEntry(tw, "manifest.json", mj); err != nil {
		return err
	}
	names := make([]string, 0, len(b.data))
	for n := range b.data {
		names = append(names, n)
	}
	sort.Strings(names) // deterministic output
	for _, n := range names {
		if err := writeEntry(tw, "files/"+n, b.data[n]); err != nil {
			return err
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return gz.Close()
}

func writeEntry(tw *tar.Writer, name string, data []byte) error {
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(data))}); err != nil {
		return err
	}
	_, err := tw.Write(data)
	return err
}

// Open reads a bundle, verifying each file's SHA-256 against the manifest.
func Open(r io.Reader) (*Bundle, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return nil, err
	}
	tr := tar.NewReader(gz)
	b := &Bundle{data: map[string][]byte{}}
	haveManifest := false
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			return nil, err
		}
		switch {
		case hdr.Name == "manifest.json":
			if err := json.Unmarshal(data, &b.Manifest); err != nil {
				return nil, fmt.Errorf("parse manifest: %w", err)
			}
			haveManifest = true
		case strings.HasPrefix(hdr.Name, "files/"):
			b.data[strings.TrimPrefix(hdr.Name, "files/")] = data
		}
	}
	if !haveManifest {
		return nil, fmt.Errorf("bundle has no manifest.json")
	}
	for _, f := range b.Manifest.Files {
		d, ok := b.data[f.Name]
		if !ok {
			return nil, fmt.Errorf("bundle missing file %q", f.Name)
		}
		sum := sha256.Sum256(d)
		if hex.EncodeToString(sum[:]) != f.SHA256 {
			return nil, fmt.Errorf("sha256 mismatch for %q", f.Name)
		}
	}
	return b, nil
}

// FlashFiles returns the images to flash, sorted by offset.
func (b *Bundle) FlashFiles() []partition.FlashFile {
	out := make([]partition.FlashFile, 0, len(b.Manifest.Files))
	for _, f := range b.Manifest.Files {
		out = append(out, partition.FlashFile{Offset: f.Offset, Name: f.Name, Data: b.data[f.Name]})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Offset < out[j].Offset })
	return out
}

// File returns a file's bytes by name.
func (b *Bundle) File(name string) ([]byte, bool) {
	d, ok := b.data[name]
	return d, ok
}
