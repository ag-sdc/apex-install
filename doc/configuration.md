# Configuration Guide

`apex-install` uses an INI-style configuration file to define one or more Forgejo APEX package registries. The tool searches all defined repositories in the order they appear in the file.

## File Location

The tool will look for the configuration file in the following locations, prioritizing the first one it finds:
1. `/etc/apex/repo.conf` (System-wide default)
2. `./repo.conf` (Local fallback)

## Configuration Format

The configuration file must use INI sections to define each repository. The section name dictates the priority (the first section parsed has the highest priority during dependency resolution conflicts).

### Section Definition
* `[Repository_Name]`: Defines the start of a new repository block.

### Keys
| Key | Required | Description |
|---|---|---|
| `REPO_URL` | Yes | The absolute base URL to your Forgejo package registry. Example: `https://git.example.com/api/packages/myowner/apex` |
| `REPO_NAME` | Yes | The internal name of the APEX repository to sync from (e.g., `myrepo`). |
| `ARCH` | No | Overrides the architecture to search for. If empty, the tool falls back to the package-level definitions. |
| `AUTH_TOKEN` | No | A Personal Access Token (PAT). Used as a `Bearer` token for private registries. |
| `AUTH_BASIC` | No | A Base64 encoded string of `username:password`. Used for Basic Authentication. |

> [!WARNING]
> Do **not** define both `AUTH_TOKEN` and `AUTH_BASIC`. If both are provided, `AUTH_TOKEN` will take precedence.

## Example `repo.conf`

```ini
# /etc/apex/repo.conf

# This repository is parsed first, so its packages have the highest priority.
[Stable]
REPO_URL=https://forgejo.mycompany.internal/api/packages/sysadmin/apex
REPO_NAME=stable
ARCH=x86_64
AUTH_TOKEN=0a1b2c3d4e5f6g7h8i9j0k

# Fallback repository for older or community-maintained packages
[Community]
REPO_URL=https://git.community.org/api/packages/public/apex
REPO_NAME=community
ARCH=x86_64
```
