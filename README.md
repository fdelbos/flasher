# flasher

A pure-Go toolkit for flashing Espressif ESP32 chips over serial — no Python,
no `esptool`, no esp-idf. Library-first, cross-platform (Linux/macOS/Windows),
with a thin CLI on top.

> Status: early. Implements the ESP serial-bootloader protocol (SLIP framing,
> reset + SYNC, register reads, chip identity). Flashing, the stub loader, and
> the portable flash-bundle format are in progress.

## Why

Rust has [`espflash`](https://github.com/esp-rs/espflash), Python has
[`esptool`](https://github.com/espressif/esptool), C has
[`esp-serial-flasher`](https://github.com/espressif/esp-serial-flasher) — Go had
no maintained, library-grade option. This fills that gap so you can flash an
ESP32 from any Go program, anywhere, with zero external toolchain.

## Layout

- `esp/` — the serial-bootloader protocol: SLIP, commands, reset/SYNC, registers,
  the stub loader, flash read/write. The `Transport` interface keeps it decoupled
  from any one serial library.
- `cmd/flasher/` — the CLI.

Planned: `partition/` (partition-table + `flasher_args.json`), `nvs/` (NVS image
generation), `bundle/` (portable pack/unpack flashable archives — also the OTA
artifact), `monitor/` (serial console).

## Try it

```
go run ./cmd/flasher list              # list available serial devices
go run ./cmd/flasher chip-info         # auto-detects the port
go run ./cmd/flasher chip-info /dev/cu.usbserial-110
```

`chip-info` connects to the ROM bootloader (no firmware flashed) and prints the
board id (factory base MAC), the derived BLE MAC, and the chip id.

## License

Apache-2.0. The embedded flasher stubs originate from Espressif's
`esp-flasher-stub` (Apache-2.0 / MIT); attribution retained.
