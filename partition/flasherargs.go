// Package partition reads esp-idf build metadata (flasher_args.json) so a flash
// can be replayed exactly without esptool/esp-idf.
package partition

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// FlasherArgs mirrors esp-idf's build/flasher_args.json.
type FlasherArgs struct {
	FlashSettings struct {
		FlashMode string `json:"flash_mode"`
		FlashSize string `json:"flash_size"`
		FlashFreq string `json:"flash_freq"`
	} `json:"flash_settings"`
	FlashFiles map[string]string `json:"flash_files"`
	Extra      struct {
		Chip   string `json:"chip"`
		Stub   bool   `json:"stub"`
		Before string `json:"before"`
		After  string `json:"after"`
	} `json:"extra_esptool_args"`
}

// FlashFile is one image to write, with its contents loaded.
type FlashFile struct {
	Offset uint32
	Name   string
	Data   []byte
}

// Load parses buildDir/flasher_args.json and loads each referenced image,
// returning them sorted by flash offset.
func Load(buildDir string) (*FlasherArgs, []FlashFile, error) {
	raw, err := os.ReadFile(filepath.Join(buildDir, "flasher_args.json"))
	if err != nil {
		return nil, nil, err
	}
	var fa FlasherArgs
	if err := json.Unmarshal(raw, &fa); err != nil {
		return nil, nil, fmt.Errorf("parse flasher_args.json: %w", err)
	}
	var files []FlashFile
	for offStr, rel := range fa.FlashFiles {
		off, err := strconv.ParseUint(offStr, 0, 32)
		if err != nil {
			return nil, nil, fmt.Errorf("bad flash offset %q: %w", offStr, err)
		}
		data, err := os.ReadFile(filepath.Join(buildDir, rel))
		if err != nil {
			return nil, nil, fmt.Errorf("read %s: %w", rel, err)
		}
		files = append(files, FlashFile{Offset: uint32(off), Name: rel, Data: data})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Offset < files[j].Offset })
	return &fa, files, nil
}

// FlashSizeBytes converts a flash_size string like "8MB" to bytes.
func FlashSizeBytes(s string) uint32 {
	s = strings.ToUpper(strings.TrimSpace(s))
	mult := uint32(1)
	switch {
	case strings.HasSuffix(s, "MB"):
		mult, s = 1024*1024, s[:len(s)-2]
	case strings.HasSuffix(s, "KB"):
		mult, s = 1024, s[:len(s)-2]
	}
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 4 * 1024 * 1024 // sensible default
	}
	return uint32(n) * mult
}
