# Baobab

An Arduino terminal app for Zed. Created by Bartosz Wiaderek. Built with Go, Bubble Tea, and Arduino CLI.

## Quick start

1. In Zed, run **task: spawn → Baobab: Open Arduino menu**. The task saves files and selects the sketch containing the active file. You can also run `bao` from a workspace or pass a path, such as `bao "W02/Display/Display.ino"`.
2. Press **S** or choose **Setup**, then select a sketch, board model, and USB port. Setup highlights your saved choices. Press **Ctrl+R** to scan for USB ports again, or choose **Set up USB later** if the board is unplugged.
3. Use **Build** to compile or **Upload** to build and flash. Set the monitor speed under **Board → Serial baud rate** to match `Serial.begin(...)`. Press **Ctrl+C** to leave the monitor.

Press the letter shown next to any menu item to open it directly. In the main menu, **C** builds, **U** uploads, and **M** opens the serial monitor. Use **Tab** and **Shift+Tab** to move through menus and selection lists, and **Enter** to choose. In selection lists, typing still filters the results.

Baobab remembers the last sketch for each workspace and the board, port, and baud rate for each sketch. An explicitly opened sketch takes precedence. Running `bao` from your home directory reopens the last workspace. On macOS, settings are stored in `~/Library/Application Support/baobab/profiles.json`. Older profiles are imported automatically.

## Builds and packages

- Builds reuse compiler objects for the same sketch and board configuration. The build report shows how many library objects were compiled or reused. Dependencies are checked on every build.
- **Tools → Upload Speed** sets the serial upload rate for the current sketch. Choose a preset, enter a custom rate, or select **Board default** to clear the override. This setting applies when the board's uploader uses `upload.speed`. The serial monitor has its own baud rate.
- Press **L** to switch between brief and full output. Use **Esc** to cancel an operation. **Tools → Clean build** forces a rebuild. If library discovery stalls during a cached build, Baobab retries once with a clean build.
- **Libraries** and **Packages** share the Arduino CLI installation. Use **Packages → Popular boards** or **Add index URL** to add board support. **Tools → Diagnostics** lists the active sketch, core, ports, and cache directory.

## Development

Install the Go version specified in `go.mod` and make `arduino-cli` available on PATH. Build from the project directory:

```sh
go build -o bao .
./bao --version
```

To run `bao` from anywhere, add the build directory to PATH or create a symlink to the binary from a directory on PATH. Zed task configuration is in `zed-task.json`.

`main.go` handles Bubble Tea events, `view.go` renders the terminal UI, and `setup.go` drives the setup wizard. `preferences.go` stores workspace and sketch selections. `arduino.go` runs Arduino CLI.
