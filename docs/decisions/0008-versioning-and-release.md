# 0008. Version with SemVer, generate the changelog, and automate releases

- Status: Accepted (2026-10-27). Semantic Versioning, a `CHANGELOG.md` generated from Conventional Commits and automated releases were the user's decision; the tools and distribution channels below were proposed and then accepted on review.
- Date: October 27, 2026
- Deciders: Bastian Kuberek

## Context

`spmeta` is published for others, and its JSON schema and exit codes are public contract ([ADR 0001](./0001-audience-build-vs-wrap.md)). Users install it from Homebrew, Linux packages, release archives, or `go install`. Version numbers must tell users when the contract changes, and release notes must not depend on someone remembering to write them.

## Decision

### Accepted: versioning and changelog

- Versions follow [Semantic Versioning 2.0.0](https://semver.org/), starting at v0.1.0. Tags are `vMAJOR.MINOR.PATCH`.
- Every commit on `main` follows [Conventional Commits 1.0.0](https://www.conventionalcommits.org/en/v1.0.0/). Allowed types: `feat`, `fix`, `docs`, `refactor`, `test`, `chore`, `ci`, `perf`. A breaking change uses `!` after the type or a `BREAKING CHANGE:` footer.
- The version bump derives from the commits since the last release:

  | Commits since last release | Bump at 1.0.0 and later | Bump before 1.0.0 |
  | --- | --- | --- |
  | Only `fix` or `perf` | Patch | Patch |
  | At least one `feat` | Minor | Minor |
  | At least one breaking change | Major | **Minor** |
  | Only `docs`, `refactor`, `test`, `chore`, `ci` | No release | No release |

  Before 1.0.0, a breaking change bumps the minor version, not the major version.
- A breaking change includes any incompatible change to the JSON schema (which also increments `schema_version`), an exit-code meaning, a flag, or a configuration key.
- `CHANGELOG.md` is generated from commit messages; nobody edits it by hand.
- The version is injected at build time with `-ldflags "-X main.version=<version> -X main.commit=<sha> -X main.date=<date>"` and printed by `spmeta version` and `spmeta --version`.
- CI checks that every commit in a pull request, and the pull request title, follow Conventional Commits.
- Releases are automated: merging the release pull request tags the version and publishes the artifacts.

### Tools and channels

- **release-please** (`googleapis/release-please-action`) maintains `CHANGELOG.md` and a release pull request, bumps the version, and creates the tag and GitHub Release when the release pull request merges. Configuration: release type `go`, `bump-minor-pre-major: true`. The first release is forced to v0.1.0 with a `Release-As: 0.1.0` commit footer.
- **goreleaser** (v2) runs in the same workflow after release-please reports a created release, builds the binaries, and uploads them to the existing GitHub Release. Its changelog is disabled (`changelog.disable: true`) and `release.mode: keep-existing` keeps the release-please notes, so `CHANGELOG.md` is the single source of truth.
- **Conventional Commits check**: `amannn/action-semantic-pull-request` validates the pull request title, and a shell step validates each commit subject in the pull request against `^(feat|fix|docs|refactor|test|chore|ci|perf)(\([a-z0-9-]+\))?!?: .+`.
- **Merge strategy**: "Rebase and merge," so every work-unit commit lands on `main` and appears in the changelog by type.
- **Targets**: `darwin` and `linux` on `arm64` and `amd64`, `CGO_ENABLED=0`.
- **Channels**: GitHub Releases (archives and checksums), the Homebrew tap `bkuberek/homebrew-tap` (cask `spmeta`, installed with `brew install --cask bkuberek/tap/spmeta`), `.deb` and `.rpm` through goreleaser's nfpm integration, and `go install github.com/bkuberek/spmeta/cmd/spmeta@latest`.
- **License**: MIT (recommendation; the user must confirm before the first release).

Publishing the repository itself is a later human decision. The full procedure is in [release.md](../design/release.md).

## Consequences

- Commit discipline is enforced by CI; a non-conforming commit blocks the pull request.
- Release-please opens the release pull request only after a releasable commit (`feat`, `fix`, `perf`, or breaking) lands on `main`.
- Tags created with the workflow's `GITHUB_TOKEN` do not trigger other workflows, so goreleaser runs as a second job of the release-please workflow instead of on a separate tag-push workflow.
- goreleaser's `brews` (formula) section is deprecated in favor of `homebrew_casks` (soft since v2.10, hard since v2.16, per [goreleaser deprecations](https://goreleaser.com/deprecations/)), so v1 publishes a cask. Homebrew supports casks with Linux binaries since 4.5.0 ("preliminary"), and the generated cask has Linux builds; the `.deb`, `.rpm`, and archive channels do not depend on that. Because the binaries are not signed, the cask removes the quarantine flag in a post-install hook, which goreleaser documents as a security bypass; revisit when macOS signing and notarization are set up. (Updated 2026-10-29; this replaces the earlier plan to keep `brews`.)
- macOS binaries are not signed or notarized in v1. Homebrew installs (through the hook) and `go install` installs are not quarantined; a downloaded archive needs `xattr -d com.apple.quarantine`.

## Alternatives considered

| Alternative | Why not |
| --- | --- |
| git-cliff for changelog generation, with manual tagging | Generates a good `CHANGELOG.md` locally or in CI, but the version bump and tag stay manual |
| goreleaser's built-in changelog only | Produces release notes but no `CHANGELOG.md` file in the repository |
| semantic-release | Requires a Node toolchain in a Go repository |
| Hand-written changelog | Drifts from the commits and depends on discipline at release time |
| Squash merge with only the pull request title checked | Collapses each feature into one changelog entry and loses the work-unit history |
| A homebrew-core formula | Requires notability and review; a tap is under the user's control |
