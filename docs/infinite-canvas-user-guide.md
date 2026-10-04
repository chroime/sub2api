# Infinite Canvas User Guide

Infinite Canvas is a local, node-based workspace for composing image prompts,
generation settings, and generated results. Open it from **Infinite Canvas** in
the user navigation, or visit `/infinite-canvas` directly.

## Before You Generate

1. Create or select an API Key that belongs to an active image-capable group.
   The key picker only lists eligible keys. Text-only, inactive, unsupported,
   or otherwise unavailable groups are excluded.
2. If no eligible key is available, use **创建 Key** in the empty state.
   Select an active image-capable group before creating the key.
3. Add a prompt node and a configuration node. Set the model and any supported
   options, then connect the nodes or use the node's generate action.

The selected credential is held in page memory while the page is open. A key
that is disabled or rejected after the page loads is reported as an invalid key;
select a current eligible key and start the generation again.

## Projects and Local Persistence

Projects and generated image assets are stored in the browser's IndexedDB for
the signed-in browser profile. Reloading the page restores the saved projects
and assets, while the API Key list is fetched again from the server. Clearing
site data, using a different browser profile, or using private browsing can
remove access to locally stored projects.

Project changes, node positions, viewport settings, and image metadata are
saved locally. Generated image bytes are kept as local assets; preview URLs are
temporary Blob URLs rebuilt when a project is opened and are not persisted.

## Export and Import

Use the project sidebar's **Export** control to download the active project. The
download is a `.canvas.zip` archive containing a validated `project.json`
manifest and the referenced image assets. Use the sidebar's **Import** control
to load an archive as a new project;
asset identifiers are remapped so the imported project does not overwrite an
existing project or asset.

Export and import validate project structure, node and edge references, asset
MIME types, archive paths, and archive size. Invalid or oversized archives are
rejected without leaving partial project or asset records behind.

Full API Key secrets are never written to project persistence and are never
included in an export archive. Export sanitizes credential-like fields, and an
archive containing a bearer token or secret field is rejected. Project files
may retain the selected key's numeric identifier for UI context, but that does
not grant access to the credential.

## Troubleshooting

- **No image key available:** create or select a key in an active image-capable
  group, then refresh the key picker if group permissions changed.
- **API Key is invalid:** the key may have been disabled after the page loaded;
  choose another eligible key and retry.
- **Generation failed:** inspect the image node error, verify the model and
  quota, and retry from the same prompt/configuration pair.
- **Image preview is missing:** reload the project so the local asset can be
  hydrated again. If the asset was deleted with browser site data, regenerate
  the image or import a project archive containing it.

Cloud synchronization is intentionally deferred. The current release treats
the browser's local IndexedDB as the project boundary; it does not upload
project files or image assets for cross-device synchronization.
