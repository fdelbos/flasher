// Command flasher is a pure-Go ESP serial-bootloader tool (no esptool / esp-idf).
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/fdelbos/flasher/esp"
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
	default:
		usage()
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage:")
	fmt.Fprintln(os.Stderr, "  flasher list              list available serial devices")
	fmt.Fprintln(os.Stderr, "  flasher chip-info [port]  read chip identity (auto-detects port if omitted)")
	os.Exit(2)
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
	var port string
	if len(args) >= 1 {
		port = args[0]
	} else {
		p, err := esp.DetectPort()
		if err != nil {
			fatal(err)
		}
		port = p
		fmt.Printf("auto-detected port: %s\n", port)
	}

	t, err := esp.OpenSerial(port, esp.ROMBaud)
	if err != nil {
		fatal(err)
	}
	l := esp.NewLoader(t)
	defer l.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	fmt.Printf("connecting to %s ...\n", port)
	if err := l.Connect(ctx); err != nil {
		fatal(err)
	}
	fmt.Println("connected.")

	macs, err := l.MACs()
	if err != nil {
		fatal(err)
	}
	fmt.Printf("board id:      %s   (base MAC, = airlock hardware_serial)\n", esp.HexID(macs.WiFiSTA))
	fmt.Printf("WiFi/base MAC: %s\n", esp.FormatMAC(macs.WiFiSTA))
	fmt.Printf("BT/BLE MAC:    %s\n", esp.FormatMAC(macs.BT))

	if id, err := l.ChipID(); err == nil {
		label := "unknown"
		if id == esp.ChipIDESP32C6 {
			label = "esp32c6"
		}
		fmt.Printf("chip id:       %d (%s)\n", id, label)
	}

	l.HardReset()
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}
