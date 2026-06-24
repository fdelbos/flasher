// Command flasher is a pure-Go ESP serial-bootloader tool (no esptool / esp-idf).
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/fdelbos/flasher/bundle"
	"github.com/fdelbos/flasher/esp"
	"github.com/fdelbos/flasher/partition"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	switch os.Args[1] {
	case "list":
		cmdList()
	case "chip-info":
		cmdChipInfo(os.Args[2:])
	case "info":
		cmdInfo(os.Args[2:])
	case "monitor":
		cmdMonitor(os.Args[2:])
	case "flash":
		cmdFlash(os.Args[2:])
	case "erase":
		cmdErase(os.Args[2:])
	case "read":
		cmdRead(os.Args[2:])
	case "pack":
		cmdPack(os.Args[2:])
	case "unpack":
		cmdUnpack(os.Args[2:])
	default:
		usage()
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage:")
	fmt.Fprintln(os.Stderr, "  flasher list                       list available serial devices")
	fmt.Fprintln(os.Stderr, "  flasher chip-info [port]           read chip identity (auto-detects port)")
	fmt.Fprintln(os.Stderr, "  flasher info [port]                identity + security/lockdown state")
	fmt.Fprintln(os.Stderr, "  flasher monitor [flags] [port]     watch serial output (ctrl-c to quit)")
	fmt.Fprintln(os.Stderr, "      --baud N   baud rate (default 115200)")
	fmt.Fprintln(os.Stderr, "      --out F    also write output to file F")
	fmt.Fprintln(os.Stderr, "      --reset    reset the board first to capture boot logs")
	fmt.Fprintln(os.Stderr, "  flasher flash [flags] <build-dir>  flash an esp-idf build (flasher_args.json)")
	fmt.Fprintln(os.Stderr, "      --port P   serial port (auto-detect if omitted)")
	fmt.Fprintln(os.Stderr, "      --baud N   flash baud rate (default 460800)")
	fmt.Fprintln(os.Stderr, "      --dry-run  print the flash plan and exit")
	fmt.Fprintln(os.Stderr, "  flasher erase [flags]              erase whole flash (stub)")
	fmt.Fprintln(os.Stderr, "      --port P            serial port")
	fmt.Fprintln(os.Stderr, "      --region OFF:SIZE   erase only a 4KiB-aligned region (hex ok)")
	fmt.Fprintln(os.Stderr, "  flasher read [--port P] <off> <size> <file>   dump flash to a file (stub)")
	fmt.Fprintln(os.Stderr, "  flasher pack [-o out] [--version V] <build-dir>   build a portable .fbundle")
	fmt.Fprintln(os.Stderr, "  flasher unpack [-C dir] <bundle>                  extract a .fbundle")
	os.Exit(2)
}

// loadFlashSource reads flash images from either an esp-idf build dir or a bundle.
func loadFlashSource(path string) (chip, mode, size, freq string, files []partition.FlashFile) {
	info, err := os.Stat(path)
	if err != nil {
		fatal(err)
	}
	if info.IsDir() {
		fa, ff, err := partition.Load(path)
		if err != nil {
			fatal(err)
		}
		return fa.Extra.Chip, fa.FlashSettings.FlashMode, fa.FlashSettings.FlashSize, fa.FlashSettings.FlashFreq, ff
	}
	f, err := os.Open(path)
	if err != nil {
		fatal(err)
	}
	defer f.Close()
	b, err := bundle.Open(f)
	if err != nil {
		fatal(err)
	}
	m := b.Manifest
	return m.Chip, m.FlashMode, m.FlashSize, m.FlashFreq, b.FlashFiles()
}

func cmdPack(args []string) {
	fs := flag.NewFlagSet("pack", flag.ExitOnError)
	out := fs.String("o", "", "output bundle file")
	version := fs.String("version", "", "version label")
	_ = fs.Parse(args)
	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "usage: flasher pack [flags] <build-dir>")
		os.Exit(2)
	}
	buildDir := fs.Arg(0)
	b, err := bundle.FromBuildDir(buildDir, *version, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		fatal(err)
	}
	outPath := *out
	if outPath == "" {
		outPath = defaultBundleName(buildDir)
	}
	f, err := os.Create(outPath)
	if err != nil {
		fatal(err)
	}
	defer f.Close()
	if err := b.Pack(f); err != nil {
		fatal(err)
	}
	fmt.Printf("packed %d files -> %s (chip %s)\n", len(b.Manifest.Files), outPath, b.Manifest.Chip)
	for _, file := range b.Manifest.Files {
		fmt.Printf("  0x%06x  %-15s %s\n", file.Offset, file.Role, file.Name)
	}
}

func cmdUnpack(args []string) {
	fs := flag.NewFlagSet("unpack", flag.ExitOnError)
	dir := fs.String("C", ".", "output directory")
	_ = fs.Parse(args)
	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "usage: flasher unpack [-C dir] <bundle>")
		os.Exit(2)
	}
	f, err := os.Open(fs.Arg(0))
	if err != nil {
		fatal(err)
	}
	defer f.Close()
	b, err := bundle.Open(f)
	if err != nil {
		fatal(err)
	}
	if err := os.MkdirAll(*dir, 0o755); err != nil {
		fatal(err)
	}
	mj, _ := json.MarshalIndent(b.Manifest, "", "  ")
	if err := os.WriteFile(filepath.Join(*dir, "manifest.json"), mj, 0o644); err != nil {
		fatal(err)
	}
	for _, file := range b.Manifest.Files {
		d, _ := b.File(file.Name)
		if err := os.WriteFile(filepath.Join(*dir, file.Name), d, 0o644); err != nil {
			fatal(err)
		}
	}
	fmt.Printf("unpacked %d files + manifest.json -> %s\n", len(b.Manifest.Files), *dir)
}

func defaultBundleName(buildDir string) string {
	base := filepath.Base(filepath.Clean(buildDir))
	if base == "build" {
		base = filepath.Base(filepath.Dir(filepath.Clean(buildDir)))
	}
	if base == "" || base == "." || base == string(filepath.Separator) {
		base = "firmware"
	}
	return base + ".fbundle"
}

func loadStub(l *esp.Loader) {
	fmt.Print("loading stub ... ")
	if err := l.RunStub(); err != nil {
		fatal(fmt.Errorf("this command needs the stub loader: %w", err))
	}
	fmt.Println("ok")
}

func cmdErase(args []string) {
	fs := flag.NewFlagSet("erase", flag.ExitOnError)
	portFlag := fs.String("port", "", "serial port")
	region := fs.String("region", "", "erase only OFFSET:SIZE instead of the whole chip")
	_ = fs.Parse(args)
	port := *portFlag
	if port == "" {
		port = resolvePort(fs.Args())
	}
	l := connect(port)
	defer l.Close()
	loadStub(l)

	if *region != "" {
		off, size, err := parseRegion(*region)
		if err != nil {
			fatal(err)
		}
		fmt.Printf("erasing region 0x%x .. +0x%x ...\n", off, size)
		if err := l.EraseRegion(off, size); err != nil {
			fatal(err)
		}
	} else {
		fmt.Println("erasing entire flash (may take a while) ...")
		if err := l.EraseFlash(); err != nil {
			fatal(err)
		}
	}
	fmt.Println("done.")
	l.HardReset()
}

func cmdRead(args []string) {
	fs := flag.NewFlagSet("read", flag.ExitOnError)
	portFlag := fs.String("port", "", "serial port")
	_ = fs.Parse(args)
	rest := fs.Args()
	if len(rest) < 3 {
		fmt.Fprintln(os.Stderr, "usage: flasher read [--port P] <offset> <size> <file>")
		os.Exit(2)
	}
	off, err := strconv.ParseUint(rest[0], 0, 32)
	if err != nil {
		fatal(fmt.Errorf("bad offset %q: %w", rest[0], err))
	}
	size, err := strconv.ParseUint(rest[1], 0, 32)
	if err != nil {
		fatal(fmt.Errorf("bad size %q: %w", rest[1], err))
	}
	outfile := rest[2]

	port := *portFlag
	if port == "" {
		port = resolvePort(nil)
	}
	l := connect(port)
	defer l.Close()
	loadStub(l)

	fmt.Printf("reading 0x%x bytes @ 0x%x ...\n", size, off)
	data, err := l.ReadFlash(uint32(off), uint32(size), func(done, total int) {
		if done%0x10000 == 0 || done == total {
			fmt.Printf("\r  %d/%d bytes", done, total)
		}
	})
	fmt.Println()
	if err != nil {
		fatal(err)
	}
	if err := os.WriteFile(outfile, data, 0o644); err != nil {
		fatal(err)
	}
	fmt.Printf("wrote %d bytes to %s\n", len(data), outfile)
	l.HardReset()
}

func parseRegion(s string) (offset, size uint32, err error) {
	a, b, ok := strings.Cut(s, ":")
	if !ok {
		return 0, 0, fmt.Errorf("region must be OFFSET:SIZE, got %q", s)
	}
	off, err := strconv.ParseUint(a, 0, 32)
	if err != nil {
		return 0, 0, fmt.Errorf("bad region offset %q: %w", a, err)
	}
	sz, err := strconv.ParseUint(b, 0, 32)
	if err != nil {
		return 0, 0, fmt.Errorf("bad region size %q: %w", b, err)
	}
	return uint32(off), uint32(sz), nil
}

func cmdFlash(args []string) {
	fs := flag.NewFlagSet("flash", flag.ExitOnError)
	baud := fs.Int("baud", 921600, "flash baud rate")
	portFlag := fs.String("port", "", "serial port (auto-detect if empty)")
	dryRun := fs.Bool("dry-run", false, "print the plan, do not write")
	_ = fs.Parse(args)
	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "usage: flasher flash [flags] <build-dir | bundle>")
		os.Exit(2)
	}
	chip, fmode, fsize, ffreq, files := loadFlashSource(fs.Arg(0))
	fmt.Printf("chip: %s   flash: %s / %s / %s\n", chip, fmode, fsize, ffreq)
	fmt.Println("plan:")
	for _, f := range files {
		fmt.Printf("  0x%06x  %-34s %9d bytes\n", f.Offset, f.Name, len(f.Data))
	}
	if *dryRun {
		return
	}

	port := *portFlag
	if port == "" {
		port = resolvePort(nil)
	}
	l := connect(port)
	defer l.Close()

	fmt.Print("loading stub ... ")
	if err := l.RunStub(); err != nil {
		fmt.Printf("failed (%v); using ROM mode\n", err)
	} else {
		fmt.Println("ok")
	}

	if err := l.SpiAttach(); err != nil {
		fatal(err)
	}
	if err := l.SpiSetParams(partition.FlashSizeBytes(fsize)); err != nil {
		fatal(err)
	}
	if *baud != esp.ROMBaud {
		fmt.Printf("switching to %d baud ...\n", *baud)
		if err := l.ChangeBaud(*baud); err != nil {
			fatal(err)
		}
	}

	for _, f := range files {
		fmt.Printf("writing %-34s @ 0x%06x (%d bytes)\n", f.Name, f.Offset, len(f.Data))
		err := l.WriteFlash(f.Offset, f.Data, func(done, total int) {
			if done%64 == 0 || done == total {
				fmt.Printf("\r  %d/%d blocks", done, total)
			}
		})
		fmt.Println()
		if err != nil {
			fatal(err)
		}
		fmt.Println("  verified (md5) ok")
	}
	_ = l.FlashFinish(false) // best-effort; HardReset reboots regardless
	fmt.Println("done. resetting into app.")
	l.HardReset()
}

func cmdMonitor(args []string) {
	fs := flag.NewFlagSet("monitor", flag.ExitOnError)
	baud := fs.Int("baud", 115200, "baud rate")
	out := fs.String("out", "", "also write output to this file")
	reset := fs.Bool("reset", false, "reset the board to capture boot logs")
	_ = fs.Parse(args)
	port := resolvePort(fs.Args())

	w := io.Writer(os.Stdout)
	if *out != "" {
		f, err := os.Create(*out)
		if err != nil {
			fatal(err)
		}
		defer f.Close()
		w = io.MultiWriter(os.Stdout, f)
	}

	ctx, cancel := context.WithCancel(context.Background())
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sig
		fmt.Fprintln(os.Stderr, "\nstopping.")
		cancel()
	}()

	fmt.Fprintf(os.Stderr, "monitoring %s @ %d (ctrl-c to quit)\n", port, *baud)
	if err := esp.Monitor(ctx, port, *baud, w, *reset); err != nil && ctx.Err() == nil {
		fatal(err)
	}
}

func cmdList() {
	ports, err := esp.ListPorts()
	if err != nil {
		fatal(err)
	}
	n := 0
	for _, p := range ports {
		if !p.IsUSB {
			continue
		}
		n++
		vendor := p.Vendor
		if vendor == "" {
			vendor = "unknown"
		}
		fmt.Printf("%-26s %s:%s  %-22s %s\n", p.Name, p.VID, p.PID, vendor, p.Product)
	}
	if n == 0 {
		fmt.Println("no USB serial devices found")
	}
}

func cmdChipInfo(args []string) {
	l := connect(resolvePort(args))
	defer l.Close()
	printIdentity(l)
	if si, err := l.SecurityInfo(); err == nil {
		printChipID(si)
	}
	l.HardReset()
}

func cmdInfo(args []string) {
	l := connect(resolvePort(args))
	defer l.Close()
	macs := printIdentity(l)
	fmt.Printf("WiFi AP MAC:      %s\n", esp.FormatMAC(macs.WiFiAP))
	fmt.Printf("ETH MAC:          %s\n", esp.FormatMAC(macs.ETH))

	si, err := l.SecurityInfo()
	if err != nil {
		fatal(err)
	}
	printChipID(si)

	fmt.Println("security:")
	fmt.Printf("  secure boot:      %s\n", onoff(si.SecureBoot()))
	fmt.Printf("  flash encryption: %s\n", onoff(si.FlashEncryption()))
	fmt.Printf("  secure download:  %s\n", onoff(si.SecureDownload()))
	r0, r1, r2 := si.KeyRevocations()
	fmt.Printf("  key revoke:       0:%t 1:%t 2:%t\n", r0, r1, r2)
	fmt.Printf("  raw flags:        0x%08x   crypt_cnt: 0x%02x\n", si.Flags, si.FlashCryptCnt)
	fmt.Println("  key slots:")
	for i := 0; i < 6 && i < len(si.KeyPurposes); i++ { // C6 has 6 key blocks
		fmt.Printf("    block %d: %s\n", i, esp.KeyPurposeName(si.KeyPurposes[i]))
	}

	l.HardReset()
}

// --- shared helpers ---

func resolvePort(args []string) string {
	if len(args) >= 1 {
		return args[0]
	}
	p, err := esp.DetectPort()
	if err != nil {
		fatal(err)
	}
	fmt.Printf("auto-detected port: %s\n", p)
	return p
}

func connect(port string) *esp.Loader {
	t, err := esp.OpenSerial(port, esp.ROMBaud)
	if err != nil {
		fatal(err)
	}
	l := esp.NewLoader(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	fmt.Printf("connecting to %s ...\n", port)
	if err := l.Connect(ctx); err != nil {
		fatal(err)
	}
	fmt.Println("connected.")
	return l
}

func printIdentity(l *esp.Loader) esp.MACs {
	macs, err := l.MACs()
	if err != nil {
		fatal(err)
	}
	fmt.Printf("board id (mac):   %s\n", esp.HexID(macs.WiFiSTA))
	fmt.Printf("board id (eui64): %s\n", esp.EUI64Hex(macs.WiFiSTA))
	fmt.Printf("WiFi MAC:         %s\n", esp.FormatMAC(macs.WiFiSTA))
	fmt.Printf("BLE MAC:          %s\n", esp.FormatMAC(macs.BT))
	return macs
}

func printChipID(si *esp.SecurityInfo) {
	label := "unknown"
	if si.ChipID == esp.ChipIDESP32C6 {
		label = "esp32c6"
	}
	fmt.Printf("chip id:       %d (%s)\n", si.ChipID, label)
}

func onoff(b bool) string {
	if b {
		return "ENABLED"
	}
	return "disabled"
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}
