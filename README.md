# flasher

A pure-Go toolkit for talking to and flashing Espressif ESP32 chips over serial —
**no Python, no `esptool`, no esp-idf**. Library-first, cross-platform
(Linux/macOS/Windows), with a thin CLI on top.

Rust has [`espflash`](https://github.com/esp-rs/espflash), Python has
[`esptool`](https://github.com/espressif/esptool), C has
[`esp-serial-flasher`](https://github.com/espressif/esp-serial-flasher) — Go had
no maintained, library-grade option. This fills that gap, so any Go program (a
CLI, a provisioning service, a GUI) can drive an ESP32 with zero external
toolchain.

Validated end-to-end on the **ESP32-C6**: identity/security readout, serial
monitor, and a full bootloader + partition-table + app flash that boots.

## Install

```
go install github.com/fdelbos/flasher/cmd/flasher@latest
```

or build from a checkout: `go build ./cmd/flasher`.

## CLI

All commands auto-detect the serial port if you don't name one (it prefers known
ESP/USB-UART vendor IDs: CP210x, CH34x, FTDI, Espressif).

### `flasher list`

List available USB serial devices.

```
$ flasher list
/dev/cu.usbserial-110      10C4:EA60  Silicon Labs CP210x
```

### `flasher chip-info [port]`

Connect over the ROM bootloader (no firmware flashed) and print identity.

```
$ flasher chip-info
board id (mac):   fc012cfe77bc
board id (eui64): fc012cfffefe77bc
WiFi MAC:         fc:01:2c:fe:77:bc
BLE MAC:          fc:01:2c:fe:77:be
chip id:       13 (esp32c6)
```

- **board id (mac)** — the factory base MAC as plain hex; recommended unique board id.
- **board id (eui64)** — the same value in EUI-64 form (`ff fe` inserted).
- MACs are derived from the base MAC (C6 four-address scheme: STA=base, AP=+1,
  **BT/BLE=+2**, ETH=+3), verified against the firmware's own `BLE_INIT` log.

### `flasher info [port]`

Everything in `chip-info`, plus the **security / lockdown state** decoded from
`GET_SECURITY_INFO` — read-only, no flashing. Useful to verify Secure Boot /
Flash Encryption status (now and after you provision).

```
security:
  secure boot:      disabled
  flash encryption: disabled
  secure download:  disabled
  key revoke:       0:false 1:false 2:false
  raw flags:        0x00000000   crypt_cnt: 0x00
  key slots:
    block 0: USER/empty
    ...
```

### `flasher monitor [flags] [port]`

Watch serial output. Non-interactive; quits cleanly on Ctrl-C.

```
flasher monitor                          # attach WITHOUT resetting the device
flasher monitor --reset                   # reset first, capture boot logs
flasher monitor --out boot.log --reset    # also write output to a file
flasher monitor --baud 115200             # default baud is 115200
```

By default `monitor` opens the port with DTR/RTS deasserted, so attaching does
**not** reboot a running board. `--reset` opts into a reboot to capture boot logs.

### `flasher flash [flags] <build-dir>`

Flash an esp-idf build, replacing `idf.py flash`. Reads `<build-dir>/flasher_args.json`
and writes exactly the images it lists, at their offsets, verifying each region
with the chip's own MD5. Partitions not in `flash_files` (e.g. NVS) are left
untouched.

```
flasher flash ./build                 # auto-detect port, 460800 baud
flasher flash --dry-run ./build       # print the plan, write nothing
flasher flash --port /dev/cu.usbserial-110 --baud 115200 ./build
```

```
$ flasher flash ./build
chip: esp32c6   flash: dio / 8MB / 80m
plan:
  0x000000  bootloader/bootloader.bin              22448 bytes
  0x010000  partition_table/partition-table.bin      3072 bytes
  0x071000  ota_data_initial.bin                    8192 bytes
  0x080000  lock.bin                             2132688 bytes
writing ...
  verified (md5) ok
done. resetting into app.
```

> Build half stays esp-idf/cmake; this replaces only the flash + monitor half.
> Currently ROM-mode (1 KiB blocks); the stub loader (16 KiB blocks, ~10×
> faster) is on the roadmap.

## Library

The `esp` package is the reusable core; the CLI is a thin front-end over it.

```go
t, _ := esp.OpenSerial("/dev/cu.usbserial-110", esp.ROMBaud)
l := esp.NewLoader(t)
defer l.Close()

l.Connect(ctx)                       // reset into ROM bootloader + SYNC
macs, _ := l.MACs()                  // identity from eFuse
si, _ := l.SecurityInfo()            // secure-boot / flash-encryption state
l.WriteFlash(0x80000, appBytes, nil) // erase + program + MD5 verify
l.HardReset()
```

Design notes:
- **Transport interface** — the loader runs over anything (`OpenSerial`, a network
  bridge, a test double), not welded to one serial library.
- **Errors are returned, never `os.Exit`/`log.Fatal`** — front-ends decide policy.
- **Progress callbacks** on long operations so a GUI can show a bar.

## Layout

```
esp/         ROM/serial protocol: SLIP, reset+SYNC, registers, identity,
             security, flashing, monitor; the Transport interface.
partition/   flasher_args.json parsing.
cmd/flasher/ the CLI.
```

## Roadmap

- Stub loader (16 KiB blocks + compression) for ~10× faster flashing.
- `bundle/` — a portable, OTA-ready flash-archive format (pack/unpack).
- `nvs/` — pure-Go NVS partition image generation.
- eFuse / Secure Boot v2 + Flash Encryption provisioning.
- More chip targets (ESP32 / S3 / C3 / H2).

## License

Apache-2.0. Protocol reimplemented from public documentation; the flasher stubs
(when added) originate from Espressif's `esp-flasher-stub` (Apache-2.0 / MIT),
attribution retained.
