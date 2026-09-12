# Playful Alien Logo Animation Design

## Goal

Create an isolated SVG preview that makes the existing XenoAI alien feel more playful and cute without changing the mascot's recognizable silhouette. The preview is for review only and must not replace the production logo until explicitly approved.

## Motion Direction

The animation combines a gentle side-to-side sway with small expression beats. The mascot remains happy throughout the loop; expression changes are limited to natural blinking, one cheeky wink, and a subtle blush. No angry, sad, or alternate mouth shapes are introduced.

## 12-Second Choreography

- 0-18%: ease into a left sway and return, with one natural blink.
- 18-36%: sway right, counter-tilt the head, and play one short right-eye wink.
- 36-54%: make two small nods while the smile stays fixed; fade in a light cheek blush.
- 54-72%: perform two soft hops, with the second hop slightly higher and the star accent brightening on takeoff.
- 72-90%: use two quick, shallow side-to-side bounces and one brief head tilt for a mischievous beat.
- 90-100%: ease back to the exact initial pose for a seamless loop.

## Visual Constraints

- Keep the transparent canvas and remove any black frame or background.
- Limit displacement to about 4px, rotation to about 4 degrees, and hop height to about 6px.
- Preserve the current mascot colors, head proportions, smile, and visual center.
- Animate overlays with opacity/scale/rotation or compatible transforms; do not interpolate between unrelated path geometries.
- Keep the existing reduced-motion behavior so the preview becomes a static happy pose when requested.

## Preview Deliverables

- `output/branding/xeno-alien-playful.svg`: standalone review candidate.
- `output/branding/xeno-alien-playful-preview.html`: dark/light neutral preview surface with motion notes and a reduced-motion toggle.

These files are intentionally outside the production component and are not wired into `HomeView.vue` or the public header.
