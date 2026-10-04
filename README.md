# apexm

`apexm` is an Android APEX package manager for Linux. It resolves, downloads, and mounts APEX/CAPEX packages from Forgejo package registries.

## Features

- **Dependency Resolution**: Resolves packages by library name (`libsomething.so`) or package name
- **Version Pinning**: Install or search for specific version codes with `name@versionCode` syntax
- **Optimal Selection**: Prioritizes by repository order, microarchitecture compatibility, API level, and semantic version
- **APEX/CAPEX Support**: Handles both plain APEX and compressed CAPEX packages
- **Auto-Mounting**: Downloads, extracts, and mounts APEX payload images via loop devices, FUSE, or extraction
- **Dual Context**: Operates as root (system-wide `/apex/`) or user (`~/.apex/`)
- **Isolated Mode**: Build environments with `--isolated` and `--rootdir`
- **Authentication**: Bearer token and Basic auth for private registries

## Quick Start

```bash
# Update repository databases
sudo apexm update

# Search for packages providing a library
apexm search libvulkan.so

# Search for a specific versionCode
apexm search libvulkan.so@1300

# Install a package by library name
sudo apexm install -Sl libvulkan.so

# Install a specific versionCode
sudo apexm install com.android.vulkan@1300

# Install with architecture constraints
sudo apexm install -Sl --arch=x86_64 --max-microarch=v3 libvulkan.so

# Download without installing
apexm download libvulkan.so

# List installed packages
apexm list

# Remove a package
sudo apexm remove com.android.vulkan

# Re-mount all installed packages after reboot
sudo apexm init

# Search installed packages locally
apexm -Qs vulkan

# Query local packages for a library
apexm local libvulkan.so
```

## Operations

| Command | Flag | Description |
|---------|------|-------------|
| `install` | `-S` | Install packages by package name |
| `install` | `-Sl` | Install packages by library name |
| `remove` | `-R` | Remove packages |
| `search` | `-Ss` | Search repositories |
| `local` | `-Qs` | Search installed packages |
| `update` | `-Su` | Sync repository databases |
| `download` | `-Sd` | Download without installing |
| `local` | `-Q` | Query installed packages for a library |
| `list` | `--list` | List installed packages |
| `init` | `--init` | Re-mount all packages |

## Target Syntax & Version Pinning

Targets are treated as package names by default (e.g., `com.android.vulkan`).
When using the `-Sl` (or `-l`/`--library`) flag, targets are treated as library names (e.g., `libvulkan.so`).

Use `-y` or `--noconfirm` to skip interactive prompts and automatically select defaults.

Use `@` to pin a specific versionCode:

```bash
# Search for versionCode 1300 of packages providing libvulkan.so
apexm search libvulkan.so@1300

# Install exact versionCode
sudo apexm install com.android.vulkan@1300

# Filter remote search or local query strictly to an exact API level
apexm -Ss --api-exact 32 bzip2
apexm -Qs --api-exact 35

# Limit to a maximum API level
sudo apexm install --api 34 com.android.vulkan
```

## Options

- `-l, --library`: Treat targets as library names rather than package names
- `-y, --noconfirm`: Skip interactive prompts (auto-select defaults)
- `-n, --name`: Restrict target matching strictly to package name or show only package name in query output
- `--api-level <level>`, `--api <level>`: Highest API level to download (upper bound / maximum API level)
- `--api-level-exact <level>`, `--api-exact <level>`: Filter packages strictly to an exact Android API level (takes precedence over `--api-level`/`--api`)
- `--skip-abi-level <0|1|2|3>`: Set ABI skip level during dependency resolution (0: none, 1: Bionic libc, 2: all NDK [default], 3: NDK + VNDK)
- `--disable-skip-abis`: Alias for `--skip-abi-level 0` (do not skip any system ABIs)
- `--skip-abi-custom <entry>`: Custom libraries to skip (file path, `lib:path` mapping, library name, or directory; comma-separated or repeated)
- `--arch <architecture>`: Target architecture (required if `--max-microarch` is set)
- `--max-microarch <level>`: Highest microarchitecture level to download
- `--config <path>`: Specify custom configuration file path
- `--isolated`: Isolated mode (ignore system states or operate on rootdir)
- `--rootdir <path>`: Base directory for isolated system operations
- `-v, --verbose`: Verbose logging


## Configuration

See [doc/configuration.md](doc/configuration.md) for repository configuration.

System config: `/etc/apex/repo.conf`
User config: `~/.config/apex/repo.conf`

## Building

```bash
# Using Go directly
go build -o apexm src/*.go

# Using Meson
meson setup build
ninja -C build
```

## Documentation

- [Usage Guide](doc/usage.md)
- [Configuration Guide](doc/configuration.md)
- Man page: `man apexm`
- Shell completions: `completions/bash/apexm`

## License

See [LICENSE](LICENSE) for details.
