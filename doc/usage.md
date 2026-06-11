# Usage Guide

`apex-install` is designed to automatically download and mount an Android APEX package given the name of a shared library (`.so`) it provides.

## Basic Usage

The primary entry point is passing the shared library name to the command:

```bash
sudo apex-install <library_name.so>
```

> [!NOTE]
> `apex-install` requires `root` privileges (`sudo`) because it uses `losetup` to bind the payload image to a loop device and `mount` to attach the ext4/erofs filesystem.

### Example
To install the APEX package that provides `libvulkan.so`:
```bash
sudo apex-install libvulkan.so
```

## How It Works

1. **Resolution**: The tool fetches the `providers.tar.gz` archive from the configured Forgejo APEX registry to find which package(s) provide `libvulkan.so`.
2. **Scoring**: If multiple packages provide the same library, it scores the candidates based on:
   * **Provider Listing Priority**: The order defined in the repository.
   * **Microarchitecture**: Prefers the lowest required microarchitecture (e.g. `v1` over `v2`) to guarantee compatibility.
   * **Semantic Versioning**: Prefers the highest available package version.
3. **Download & Extract**: It downloads the `.capex` or `.apex` file and extracts its contents to `/opt/apex/<org.name>.apex`.
4. **Mounting**: It maps `apex_payload.img` to a loop device (`/dev/loopX`) and mounts it read-only to `/apex/<org.name>`.

## Errors & Troubleshooting

- **"No matching packages found"**: Ensure the library name is spelled correctly and exists in the upstream registry. Check your configured `ARCH` in `repo.conf`.
- **"losetup failed"**: Ensure you are running the command as `root` and that loop devices are enabled in your kernel.
- **"Authentication failed"**: Check your `repo.conf` file to ensure your `AUTH_TOKEN` or `AUTH_BASIC` credentials are valid.
