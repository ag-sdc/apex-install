# APEX Install

`apex-install` is an automated, system-level Linux tool for resolving and installing Android APEX packages from a Forgejo native package registry.

## Features
- **Dependency Resolution**: Automatically resolves the best APEX package based on a provided library string (`libsomething.so`).
- **Optimal Selection**: Prioritizes repository provider order, lowest compatible microarchitecture (e.g. `v8_0` < `v8_1`), and highest semantic version.
- **Auto-Mounting**: Automatically downloads, unzips, maps the internal payload image via `losetup`, and mounts it locally to `/apex/<org.name>`.
- **Authentication**: Supports fetching from private Forgejo registries using Bearer Tokens or Basic Authentication.

## Configuration
Configuration is managed via `/etc/apex/repo.conf`. See `repo.conf.sample` for details.

## Usage
```bash
# Must be run as root to configure loop devices and mount points
sudo apex-install libvulkan.so
```
