# Release

This document defines how `spmeta` is versioned, how the changelog is produced, how releases are built and published, and what the user must set up before the first release. The policy is [ADR 0008](../decisions/0008-versioning-and-release.md): Semantic Versioning, a `CHANGELOG.md` generated from Conventional Commits, and automated releases are accepted, and so are the tools named here.

## Versioning

- Versions follow Semantic Versioning, starting at v0.1.0. Tags are `vMAJOR.MINOR.PATCH`.
- Commits on `main` follow Conventional Commits with the types `feat`, `fix`, `docs`, `refactor`, `test`, `chore`, `ci`, and `perf`. A breaking change uses `!` after the type (for example `feat!: rename the artist field`) or a `BREAKING CHANGE:` footer.
- The bump derives from the commits since the last release:

  | Commits since the last release | At 1.0.0 and later | Before 1.0.0 |
  | --- | --- | --- |
  | `fix` or `perf` only | Patch | Patch |
  | At least one `feat` | Minor | Minor |
  | At least one breaking change | Major | Minor |
  | Only `docs`, `refactor`, `test`, `chore`, `ci` | No release | No release |

- Breaking changes include an incompatible JSON schema change (which also increments `schema_version`), a changed exit-code meaning, and a removed or renamed flag, environment variable, or configuration key.
- If the local-only cut is released ([ADR 0004](../decisions/0004-web-api-built-last.md)), it is v0.1.0 and the Web milestones ship as v0.2.0. Otherwise the first release, v0.1.0, includes Web mode.

## Version in the binary

`cmd/spmeta/main.go` declares:

```go
var (
	version = "dev"
	commit  = ""
	date    = ""
)
```

goreleaser sets them with `-ldflags "-s -w -X main.version={{.Version}} -X main.commit={{.ShortCommit}} -X main.date={{.Date}}"`. For `go install` builds, `version` stays `"dev"` and `main` falls back to `debug.ReadBuildInfo()` (module version, `vcs.revision`, `vcs.time`). `spmeta version` and `spmeta --version` print the result ([cli.md](./cli.md)).

## Release flow

Releases are driven by merges to `main`; nobody tags by hand.

```text
Conventional Commits merged to main (rebase and merge)
  |
  v
release-please (on every push to main)
  opens or updates the release pull request:
  CHANGELOG.md section + version bump in .release-please-manifest.json
  |
  |  a maintainer merges the release pull request
  v
release-please creates tag vX.Y.Z and the GitHub Release (notes = the CHANGELOG section)
  |
  |  same workflow, second job, only when release_created == "true"
  v
goreleaser release --clean (checks out the tag)
  builds darwin/linux x amd64/arm64, archives, checksums.txt, .deb, .rpm
  |
  +--> uploads assets to the existing GitHub Release (release.mode: keep-existing)
  +--> pushes Formula/spmeta.rb to bkuberek/homebrew-tap
  +--> the tag makes the version available to go install through the Go module proxy
```

goreleaser runs as a second job of the release-please workflow because tags created with the workflow's `GITHUB_TOKEN` do not trigger other workflows. Its changelog is disabled, so `CHANGELOG.md` and the release-please notes are the single source of truth.

## Files created in task T15

### `release-please-config.json`

```json
{
  "$schema": "https://raw.githubusercontent.com/googleapis/release-please/main/schemas/config.json",
  "packages": {
    ".": {
      "release-type": "go",
      "package-name": "spmeta",
      "include-component-in-tag": false,
      "bump-minor-pre-major": true,
      "bump-patch-for-minor-pre-major": false,
      "changelog-sections": [
        { "type": "feat", "section": "Features" },
        { "type": "fix", "section": "Bug Fixes" },
        { "type": "perf", "section": "Performance" },
        { "type": "docs", "section": "Documentation", "hidden": true },
        { "type": "refactor", "section": "Refactoring", "hidden": true },
        { "type": "test", "section": "Tests", "hidden": true },
        { "type": "chore", "section": "Chores", "hidden": true },
        { "type": "ci", "section": "CI", "hidden": true }
      ]
    }
  }
}
```

### `.release-please-manifest.json`

```json
{ ".": "0.0.0" }
```

The first release is pinned to v0.1.0 with a commit whose message carries the footer `Release-As: 0.1.0`.

### `.github/workflows/release.yml`

```yaml
name: release
on:
  push:
    branches: [main]
permissions:
  contents: write
  pull-requests: write
jobs:
  release-please:
    runs-on: ubuntu-latest
    outputs:
      release_created: ${{ steps.release.outputs.release_created }}
      tag_name: ${{ steps.release.outputs.tag_name }}
    steps:
      - id: release
        uses: googleapis/release-please-action@v4
  goreleaser:
    needs: release-please
    if: needs.release-please.outputs.release_created == 'true'
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          ref: ${{ needs.release-please.outputs.tag_name }}
          fetch-depth: 0
      - uses: actions/setup-go@v5
        with:
          go-version: stable
      - uses: goreleaser/goreleaser-action@v6
        with:
          distribution: goreleaser
          version: "~> v2"
          args: release --clean
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
          HOMEBREW_TAP_GITHUB_TOKEN: ${{ secrets.HOMEBREW_TAP_GITHUB_TOKEN }}
```

The action major versions above are from memory; task T15 pins each action to its current major version (or a commit SHA) at implementation time.

### `.goreleaser.yaml`

```yaml
version: 2
project_name: spmeta

builds:
  - id: spmeta
    main: ./cmd/spmeta
    binary: spmeta
    env: [CGO_ENABLED=0]
    goos: [darwin, linux]
    goarch: [amd64, arm64]
    flags: [-trimpath]
    ldflags:
      - -s -w -X main.version={{.Version}} -X main.commit={{.ShortCommit}} -X main.date={{.Date}}

archives:
  - formats: [tar.gz]
    name_template: "{{ .ProjectName }}_{{ .Version }}_{{ .Os }}_{{ .Arch }}"
    files: [LICENSE, README.md, CHANGELOG.md]

checksum:
  name_template: checksums.txt

changelog:
  disable: true

release:
  mode: keep-existing

nfpms:
  - package_name: spmeta
    description: Now-playing metadata for Spotify
    homepage: https://github.com/bkuberek/spmeta
    maintainer: "Bastian Kuberek <bastian@bkuberek.com>"
    license: MIT
    formats: [deb, rpm]
    bindir: /usr/bin

brews:
  - name: spmeta
    repository:
      owner: bkuberek
      name: homebrew-tap
      token: "{{ .Env.HOMEBREW_TAP_GITHUB_TOKEN }}"
    directory: Formula
    homepage: https://github.com/bkuberek/spmeta
    description: Now-playing metadata for Spotify
    license: MIT
    install: |
      bin.install "spmeta"
    test: |
      system "#{bin}/spmeta", "version"
```

Run `goreleaser check` before committing. Key names (`formats`, `directory`, `brews`) follow goreleaser v2 as remembered; `goreleaser check` is the authority. If `brews` reports a deprecation warning in favor of `homebrew_casks`, keep the formula for v1 ([ADR 0008](../decisions/0008-versioning-and-release.md)).

## Distribution channels

| Channel | Command for users | Produced by |
| --- | --- | --- |
| Homebrew (macOS, Linux) | `brew install bkuberek/tap/spmeta` | goreleaser `brews` |
| Debian, Ubuntu | `sudo apt install ./spmeta_0.1.0_linux_amd64.deb` | goreleaser `nfpms` |
| Fedora, RHEL | `sudo dnf install ./spmeta_0.1.0_linux_amd64.rpm` | goreleaser `nfpms` |
| Archive | Download from GitHub Releases, verify with `checksums.txt` | goreleaser `archives` |
| Go | `go install github.com/bkuberek/spmeta/cmd/spmeta@latest` | The tag |

macOS binaries are not signed or notarized in v1. Homebrew and `go install` installs are not quarantined. A manually downloaded archive needs `xattr -d com.apple.quarantine ./spmeta` before the first run; the README says so.

## Required from the user

The release automation needs these one-time steps by the user. No secret values belong in the repository or in these documents.

1. Decide to publish the repository, then create `github.com/bkuberek/spmeta` and push.
2. Confirm the license (MIT recommended) and the public maintainer email for Linux packages.
3. Create the repository `github.com/bkuberek/homebrew-tap` with a `Formula/` directory.
4. Create a fine-grained personal access token with "Contents: read and write" on `bkuberek/homebrew-tap` only, and store it as the Actions secret `HOMEBREW_TAP_GITHUB_TOKEN` in `bkuberek/spmeta`.
5. In `bkuberek/spmeta` settings, enable "Allow GitHub Actions to create and approve pull requests" (release-please needs it), allow only "Rebase and merge," and protect `main` with the required checks `test`, `lint`, `integration`, and `commits`.

## Release checklist

Before merging a release pull request:

1. Confirm CI is green on `main`.
2. Confirm the manual matrix rows for the shipped backends are recorded ([testing.md](./testing.md)).
3. Read the release pull request's `CHANGELOG.md` section; if a change is missing or misclassified, fix it with a follow-up commit (for example a `fix:` commit), not by editing the changelog.
4. Confirm the version bump matches the table in Versioning, in particular that breaking changes before 1.0.0 bump the minor version.
5. Merge the release pull request.

After the release workflow finishes:

1. Confirm the GitHub Release lists four archives, `checksums.txt`, two `.deb`, and two `.rpm` files.
2. Run `brew install bkuberek/tap/spmeta && spmeta version` on macOS and confirm the version.
3. Run `go install github.com/bkuberek/spmeta/cmd/spmeta@vX.Y.Z` and confirm `spmeta version`.
4. Install the `.deb` on an Ubuntu machine or container and run `spmeta version`.
