# Signal → Metadata art draft

## Objective
Create a reviewable first visual identity for spmeta: a README/social banner and a matching monochrome mark. Music becomes structured data; this is not a music player.

## Scope and authorization
- Work on `feat/signal-metadata-art`.
- Commits allowed after user approval (2026-10-29). Push, PR, and publishing remain user decisions.
- Add artwork and reference it from the README and docs index; leave the existing architecture illustration unchanged.
- Preserve existing untracked `.atl/` state.

## Direction
Bracketed now-playing triangle, lowercase monospaced wordmark, charcoal `#111716`, warm white `#F1F2EB`, amber `#F4C76B`.
Banner tagline: “Now playing. Ready to pipe.” Three fine source lines converge into a metadata strip; use real documented output rather than fictional functionality.
Editable native SVG artwork, with a PNG banner preview. No external image/font fetches, scripts, embedded HTML, or remote references in assets.

## Tasks
- [x] T1 — Create `docs/assets/spmeta-mark.svg`, `docs/assets/spmeta-banner.svg`, and `docs/assets/spmeta-banner.png`.
  - Route: delegated direct; two non-trivial artwork files and design preparation require a bounded writer.
  - Acceptance: coherent mark/banner; legible English copy; accessible SVG title/description; monochrome mark works at small sizes; no Spotify logo imitation.
- [x] T2 — Validate XML, render artwork, visually inspect the preview, and confirm branch/scope/no commits.
  - Route: writer self-check with parent structural and visual spot check; passive artwork does not require semantic code review.
  - Checks: `xmllint --noout docs/assets/spmeta-mark.svg docs/assets/spmeta-banner.svg`; `rsvg-convert -o docs/assets/spmeta-banner.png docs/assets/spmeta-banner.svg`; `magick identify docs/assets/spmeta-banner.png`; `git diff --check`.

- [x] T3 — Use the artwork: banner under the README title, asset rows and the regeneration command in `docs/README.md`.
  - Route: direct inline; two mechanical doc edits.
  - Checks: `git diff --check`; banner link resolves; artwork re-reviewed by the user ("we've created art in docs/assets let's use it").

## Verification policy
No behavioral code changes or meaningful runnable RED test; use XML, rendering, and visual checks instead. No Go test suite needed for isolated passive artwork. No RDD candidate or commit is created while human artwork review is pending.

## Delivery
Forecast: fewer than 250 authored lines; generated PNG excluded. Strategy: ask-on-risk. Running committed lines: 0. Commits: see `git log main..feat/signal-metadata-art`.

## Progress
- T1 complete: self-contained, accessible SVG mark and 1600×800 SVG banner, plus PNG preview.
- T2 writer checks passed: XML validation, PNG render, PNG dimensions, whitespace check, absence of script/foreignObject/event handlers/external references, and exact README output comparison.
- Writer visually inspected the final banner and a 24×24 mark render. The banner has clean spacing and readable copy; the small mark retains the bracket/triangle silhouette.
- Parent inspected the PNG and SVG structures, reran XML validation and whitespace checks, and confirmed the exact branch, empty staging area, unchanged tracked files, and HEAD equal to main. T2 is complete.
- No Go suite run: this draft changes passive artwork only. No files staged or committed.
- Engram mirror: `odd/signal-metadata-art/tasks`.

- 2026-10-29: user approved the artwork and asked to use it. T3 integrated it into the README and the docs index, and the work was committed on `feat/signal-metadata-art`.

## Next step
Push and open the PR when the user asks. After merge, upload `docs/assets/spmeta-banner.png` as the GitHub social preview (Settings → General → Social preview; the API does not support it).
