# Unified main branch and homepage selection

Development now uses `main`. It includes the b2 features and both homepage designs.
The previous b2 tip is retained under `archive/b2-before-homepage-unification-20261027`.
The previous main tip is retained under `backup/main-before-homepage-unification-20261027`.
Neither branch nor any database needs to be deleted.

## Runtime settings

In **System Settings → General → Homepage template**, select:

| Value | Page |
| --- | --- |
| `classic` | Previous main homepage |
| `brand` | Previous b2 homepage |
| `compact` | Minimal site branding and login/navigation |
| `custom` | Existing HTML content or external webpage URL |

Save the settings and reload the homepage. There is no rebuild or restart for a
template change. Site name, logo, subtitle, documentation/API URLs, contact
information and access rules use the same public settings. Retained custom
content does not override an explicitly selected built-in template.

**Preview homepage** opens the unsaved selection in a separate administrator-only
tab. A draft contains only the public homepage fields, expires after 30 minutes,
and is replaced by the next preview. The preview does not save server settings.

## First update from b2 or an older main deployment

Use the new `deploy/update-sub2api.sh` as the updater **before changing the old
checkout branch**. Keep a copy of the old updater. The script:

1. Validates the original checkout, deployment configuration and installed marker.
2. Backs up PostgreSQL, Redis, application files and deployment configuration;
   checks archive readability, Redis integrity and SHA-256 checksums.
3. Initializes `homepage_template` only if the key does not already exist.
4. Fetches and fast-forwards `main`, builds, recreates only the application service,
   then verifies health, the original data mount and completed installation state.

Initialization preserves legacy custom content first, then compact mode; otherwise
an old b2 checkout selects `brand` and an old main checkout selects `classic`.
An existing explicit selection is never overwritten, including on repeated runs.
For a detached checkout or another old branch, pass
`LEGACY_HOMEPAGE_TEMPLATE=brand` or `LEGACY_HOMEPAGE_TEMPLATE=classic` explicitly.

The update script requires a working Docker Compose build environment, including
Buildx when the Dockerfile uses BuildKit. Do not point the updater at a newly
created or empty data directory. Default persistent data remains
`/home/sub2api/data/app` mounted at `/app/data`.

Only tar's `file changed as we read it` diagnostics for files under `app/logs/`
may be accepted during backup. The script compares hashes of every regular
non-log application file before/after archival. Non-log changes, unreadable files,
fatal errors, corrupt backups or checksum mismatches stop the update.

## Rollback

Retain the previously running application image before deployment and keep the
verified backup path. If an application rollback is necessary, use the previous
image with the original compose environment, ports and persistent data mount.
The new settings key is ignored by old binaries. Never replace the active
database with an old dump during an ordinary application rollback: this would
discard writes made after the backup. Database restoration is a separate recovery
operation. Keep b2 frozen as a historical reference; put subsequent fixes on main.
