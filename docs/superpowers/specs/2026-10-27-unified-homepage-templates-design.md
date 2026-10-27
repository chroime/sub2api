# Unified main branch and configurable homepage

Approved in this conversation on 2026-10-27: maintain one main branch, retain both existing homepages, and select the homepage at runtime from system settings.

## Scope

- Preserve the complete current b2 feature set, including pending user balance notification records and the podium background fix.
- Integrate b2 into main while retaining both existing home designs. Keep a recoverable b2 archive tag; do not delete its branch or either database.
- Add a persisted `homepage_template` setting with `classic`, `brand`, `compact`, and `custom` values. Classic is the former main experience, brand is the former b2 story page.
- Reuse existing site name, logo, subtitle, API URL, documentation URL, contacts, authentication, and model-plaza permissions. Homepage choice changes presentation only.
- Expose the selection in admin and public settings, including first-render HTML configuration injection. Admin saves must invalidate existing public configuration caches.
- Replace the two conflicting homepage controls with a single selection and conditional custom-content editor. Existing custom HTML/URL and compact behavior remain compatible.
- Provide an administrator-only preview in a separate tab using a short-lived local draft containing public homepage settings only. Preview never persists server configuration. Invalid/expired drafts show an actionable error.
- Lazy-load templates and scope each design's styles to its root. A failed optional template must offer the compact presentation as a fallback.
- Preserve each deployment's homepage before changing its update branch to main. Existing explicit selection wins; otherwise nonempty custom content wins, then the compact setting, then the old deployment branch (`b2` -> brand, `main` -> classic). New installations default to classic.
- Update the deployment script default to main. Backup must complete and validate before setting initialization or checkout/service changes. Continuously written log files may be archived with a documented log-only warning; changes to other application files must still fail backup.

## Data and compatibility

`homepage_template` uses the existing settings key/value store and does not require a schema migration. Server reads resolve older missing settings from `home_content` and `compact_home_enabled`; a missing normal template resolves to classic after deployment initialization. An explicit valid selection always takes precedence over old content and compact flags. Invalid submitted selections return a bad request. Clients omitting the new field retain its stored value. Existing legacy fields remain readable for compatibility, but the new UI has one authoritative selection.

Before unified deployment, the update script initializes the key only if missing, using the pre-update branch and legacy settings. This is idempotent and runs after a database backup. Data directories, balances, accounts, API keys, orders, and notification configuration stay attached to their existing instances.

## Preview

The settings component snapshots the public homepage fields into a browser-local draft with a random ID and a 30-minute expiry, then opens an admin-protected preview route. Only the matching draft is read. A new preview replaces the previous draft; already-rendered previews keep their in-memory snapshot. Draft storage contains no SMTP, upstream, AstrBot, or authentication secrets. The production home route always reads saved public settings.

## Acceptance

1. All four modes render from the saved selection, including when old custom content is still present.
2. Changing the selection and reloading the home page requires no rebuild or service restart.
3. Preview shows the selected mode and draft public fields without changing persisted settings.
4. Classic and brand retain their existing visual identity and use the same visibility/authentication rules.
5. Public API and HTML-injected settings agree; partial updates and unknown enum values are tested.
6. Both existing b2 features and the new configuration pass focused backend/frontend tests, type checking, build, and browser verification.
7. Main contains the b2 history and completed changes; the retired b2 state has a named archive tag. Local service keeps `backend/data` and its installation marker.

## Deferred

Per-domain template mapping inside one instance, a drag-and-drop page builder, and new template-specific marketing fields are outside this implementation.
