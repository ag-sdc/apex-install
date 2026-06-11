# Configuration Guide

`apex-install` uses a simple configuration file to determine which Forgejo server and repository to query for packages.

## File Location

The tool will look for the configuration file in the following order:
1. `/etc/apex/repo.conf` (System-wide default)
2. `./repo.conf` (Local fallback for testing)

## Configuration Options

The configuration file uses a simple `KEY=VALUE` format. Blank lines and lines starting with `#` are ignored.

| Key | Required | Description |
|---|---|---|
| `REPO_URL` | Yes | The absolute base URL to your Forgejo package registry. Example: `https://git.example.com/api/packages/myowner/apex` |
| `REPO_NAME` | Yes | The internal name of the APEX repository to sync from (e.g., `myrepo`). |
| `ARCH` | No | Overrides the architecture to search for. If empty, the tool considers all architectures available to the packages (including `any`). |
| `AUTH_TOKEN` | No | A Personal Access Token (PAT). Used as a `Bearer` token for private Forgejo registries. |
| `AUTH_BASIC` | No | A Base64 encoded string of `username:password`. Used for Basic Authentication. |

> [!WARNING]
> Do **not** define both `AUTH_TOKEN` and `AUTH_BASIC`. If both are provided, `AUTH_TOKEN` will take precedence.

## Example `repo.conf`

```ini
# /etc/apex/repo.conf

# Registry target
REPO_URL=https://forgejo.mycompany.internal/api/packages/sysadmin/apex
REPO_NAME=stable

# Target a specific architecture if necessary
ARCH=x86_64

# Authenticate against private registry
AUTH_TOKEN=0a1b2c3d4e5f6g7h8i9j0k
```
