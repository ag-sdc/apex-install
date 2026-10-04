# Usage Guide

`apexm` is designed to automatically download and mount Android APEX packages given the name of a shared library (`.so`) it provides or the exact package name.

## Basic Usage

The primary entry point is passing an operation and the targets to the command:

```bash
apexm <operation> [options] [targets...]
```

### Context: Root vs User

`apexm` supports two execution contexts:
* **Root Mode** (`sudo apexm`): Operates on the system-wide `/apex/` directory. Packages are mounted globally.
* **User Mode** (`apexm`): Operates in `~/.apex/`. Packages are mounted in the user's home directory.

> [!NOTE]
> Root mode uses `losetup` to bind the payload image to a loop device and `mount` to attach the ext4/erofs filesystem.
> User mode relies on FUSE or extraction (via `7z` or `unzip`) since loop devices require root.

## Operations

| Operation | Short Flag | Description |
|-----------|------------|-------------|
| `install` | `-S` | Install packages and mount them. Requires target names. |
| `remove` | `-R` | Remove installed packages and unmount them. |
| `search` | `-Ss` | Search configured repositories for packages matching the target. |
| `local` | `-Qs` | Search locally installed packages matching the target. |
| `update` | `-Su` | Sync repository databases to get the latest package lists. |
| `download` | `-Sd` | Download packages without installing or mounting them. |
| `local` | `-Q` | Query locally installed packages matching the target library. |
| `list` | `--list` | List all installed packages. |
| `init` | `--init` | Re-mount all previously installed packages (useful after a reboot). |

## Target Syntax & Version Pinning

By default, `-S` treats targets as package names (`com.android.vulkan`). 
To install by library name (`libvulkan.so`), you must use the `-l` or `--library` flag (e.g., `-Sl`).

Use `@` to pin a specific versionCode:
```bash
# Pin a library
apexm install -Sl libvulkan.so@1300

# Pin a package
apexm install com.android.vulkan@1300
```

## Options

* `-l, --library`: Treat install targets as library names rather than package names.
* `-y, --noconfirm`: Skip interactive prompts (auto-selects defaults).
* `--config <path>`: Specify a custom configuration file path.
* `--verbose`: Enable verbose output for debugging.
* `--isolated`: Run in isolated mode for build environments, keeping everything local to the rootdir.
* `--rootdir <path>`: Specify the base directory for APEX storage and mounts (default is `/apex` for root, `~/.apex` for user).
* `--arch <architecture>`: Force the tool to query a specific architecture.
* `--max-microarch <level>`: Specify the highest microarchitecture level to download (e.g. `v3` for `x86_64` or `v8_2` for `aarch64`).
* `--api-level <level>`, `--api <level>`: Specify the highest Android API level to download (upper bound / maximum API level).
* `--api-level-exact <level>`, `--api-exact <level>`: Filter packages strictly to an exact Android API level. Takes precedence over `--api-level` and `--api`.
* `--skip-abi-level <0|1|2|3>`: Set ABI skip level during dependency resolution (0: no ABI skipped, 1: Bionic libc & related libs, 2: all NDK libs [default], 3: all NDK + VNDK libs).
* `--disable-skip-abis`: Alias for `--skip-abi-level 0` (do not skip any base Android ABI libraries).
* `--skip-abi-custom <entry>`: Specify custom shared libraries to skip (file path, `lib:path` mapping, library name, or directory; comma-separated or repeated).
* `-n, --name`: Restrict target matching strictly to package name (do not search library dependencies), or show only package name in query output.

## Provider Picker

When installing by library (`-Sl`) and multiple packages provide the same library, a pacman-style interactive picker is shown:

```
:: There are 2 providers available for libvulkan.so:
:: Repository stable
   1) com.android.vulkan
   2) com.google.android.vulkan
Enter a number (default=1): 
```

Use `-y` or `--noconfirm` to automatically select the default without prompting.

## Examples

To update repositories and install a library:
```bash
sudo apexm update
sudo apexm install -Sl libvulkan.so
```

To search for a specific versionCode:
```bash
apexm search libvulkan.so@1300
```

Search output format:
`repo/package version (type, arch-microarch, API level)`
Example:
`stable/com.android.vulkan 1.3.0 (capex, x86_64-v3, 34)`

To search locally installed packages:
```bash
apexm -Qs vulkan
apexm local -s libvulkan.so
apexm -Qs --name vulkan
```

Local search output format:
`local/package version (type, arch-microarch, API level)`
Example:
`local/com.android.vulkan 1.3.0 (capex, x86_64-v3, API 34)`

To install multiple dependencies prioritizing `v3` microarchitecture:
```bash
sudo apexm install -Sl --arch=x86_64 --max-microarch=v3 libvulkan.so libcamera2ndk.so
```

To filter packages strictly to an exact Android API level:
```bash
# Remote search filtered to API 32
apexm -Ss --api-exact 32 bzip2

# Local installed packages filtered to API 35
apexm -Qs --api-exact 35

# Install candidate matching an exact API level
sudo apexm install --api-level-exact 32 com.android.vulkan
```

To specify an upper bound / maximum API level:
```bash
sudo apexm install --api 34 com.android.vulkan
```

## How It Works

1. **Resolution**: The tool fetches `providers.tar.gz` from the configured Forgejo APEX registries.
2. **Selection**: If multiple packages match, it scores candidates based on repository order, microarchitecture compatibility, API level, and semantic version.
3. **Download**: It downloads the `.apex` or `.capex` (compressed APEX) file.
4. **Mounting**: 
   - For `.capex`, it first decompresses the payload.
   - Root context: Maps `apex_payload.img` to `/dev/loopX` and mounts to `/apex/<package_name>`.
   - User context: Mounts via FUSE or extracts contents directly to `~/.apex/`.

## Shell Completion

Bash completions are available in `completions/bash/apexm`. When installed via Meson, completion is automatically placed in `/usr/share/bash-completion/completions/apexm`.

To enable completion manually in your current shell:
```bash
source completions/bash/apexm
```

## Errors & Troubleshooting

- **"No matching packages found"**: Ensure the target name is spelled correctly. Try `apexm update`. Check `ARCH` in your configuration.
- **"losetup failed"**: Ensure you are running as root or have loop devices enabled.
- **"Authentication failed"**: Check your configuration file for valid `AUTH_TOKEN` or `AUTH_BASIC` credentials.
