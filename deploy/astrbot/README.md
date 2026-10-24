# AstrBot / NapCat QQ group selection

The global governance settings page reads the QQ groups currently joined by
NapCat through AstrBot's existing OneBot reverse WebSocket connection. It does
not use historical sessions or require a public NapCat HTTP port.

## AstrBot endpoint

`im-groups.patch` adds `GET /api/v1/im/groups` to AstrBot 4.29.1. Apply it to the
AstrBot source checkout, build its image, and recreate only the AstrBot service:

```sh
git -C /path/to/AstrBot apply --check /path/to/im-groups.patch
git -C /path/to/AstrBot apply /path/to/im-groups.patch
```

Keep the existing AstrBot data volume and NapCat configuration. The extension
uses the existing `im` API scope and queries `get_group_list` over the live
OneBot connection. Each AstrBot OneBot adapter must have one connected bot so
its UMO also identifies an unambiguous proactive-message destination.

Success returns current group names and sendable UMOs:

```json
{
  "status": "ok",
  "message": null,
  "data": {
    "complete": true,
    "groups": [
      {
        "bot_id": "napcat-main",
        "group_id": "123456789",
        "group_name": "Example group",
        "umo": "napcat-main:GroupMessage:123456789"
      }
    ]
  }
}
```

An offline bot, expired QQ login, timeout, or unsupported connection returns an
error instead of a partial or historical list. A successful empty list means
the live bot has no joined groups. The lookup has an eight-second overall
timeout and never sends group messages.

## Sub2API settings

Create a key with `im` permission in AstrBot **Settings → OpenAPI**. The full key
is returned only when created; the key list contains a prefix and metadata.
Sub2API saves it with AES-256-GCM using its fixed `TOTP_ENCRYPTION_KEY` or
`totp.encryption_key` setting. Preserve that encryption key with your backups.

The administrator settings API returns an empty `api_key` and an
`api_key_configured` flag. A blank key on save keeps the existing value; a new
value replaces it; `clear_api_key: true` explicitly removes it. Legacy plaintext
configuration remains readable and is migrated on the next settings save.

The browser sends a settings draft to
`POST /api/v1/admin/upstream-governance/global-settings/astrbot-groups`. The
backend queries AstrBot without saving the draft. Omitting the key can reuse
the saved credential only when the draft address matches the saved address.
Addresses ending with `/api/v1` and addresses without that suffix are both
supported. Group names fill automatically after selection; existing targets
remain in place when a refresh fails.
