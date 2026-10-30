# Release

This document defines how `spmeta` is versioned, how the changelog is produced, how releases are built and published, and what is set up outside the repository. The policy is [ADR 0008](../decisions/0008-versioning-and-release.md): Semantic Versioning, a `CHANGELOG.md` generated from Conventional Commits, and automated releases are accepted, and so are the tools named here.

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
- The first release is the local-only cut ([ADR 0004](../decisions/0004-web-api-built-last.md)): v0.1.0 ships macOS and Linux local mode, and the Web milestones ship as v0.2.0.

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
  |  a maintainer reviews and merges the release pull request (admin merge, see below)
  v
release-please creates tag vX.Y.Z and the GitHub Release (notes = the CHANGELOG section)
  |
  |  same workflow, second job, only when release_created == "true"
  v
goreleaser release --clean (checks out the tag)
  generates completions and the man page (internal/docgen)
  builds darwin/linux x amd64/arm64, archives, checksums.txt, .deb, .rpm
  |
  +--> uploads assets to the existing GitHub Release (release.mode: keep-existing)
  +--> pushes pkgs/spmeta/default.nix to bkuberek/nur-packages (skipped without NUR_GITHUB_TOKEN)
  +--> pushes Casks/spmeta.rb to bkuberek/homebrew-tap
  +--> pushes PKGBUILD and .SRCINFO to the AUR package spmeta-bin (skipped without AUR_KEY)
  +--> the tag makes the version available to go install through the Go module proxy
```

goreleaser runs as a second job of the release-please workflow because tags created with the workflow's `GITHUB_TOKEN` do not trigger other workflows. Its changelog is disabled, so `CHANGELOG.md` and the release-please notes are the single source of truth.

The release pull request is opened with the workflow's `GITHUB_TOKEN`, and GitHub does not start workflows for events caused by that token. CI therefore never runs on the release pull request, and the required checks on `main` never report on it. The maintainer reviews it and merges it with `gh pr merge --admin --rebase <number>`. This is safe because the release pull request changes only `CHANGELOG.md` and `.release-please-manifest.json`, and CI already passed on every commit it describes. A personal access token for release-please is deliberately not used.

## Files created in task T15

### `release-please-config.json`

The package settings are the ones in ADR 0008: release type `go`, no component in the tag (tags are `vX.Y.Z`), `bump-minor-pre-major: true` so a breaking change before 1.0.0 bumps the minor version, and `bump-patch-for-minor-pre-major: false` so a `feat` bumps the minor version. `docs`, `refactor`, `test`, `chore`, and `ci` sections are hidden, so those commits never open a release pull request by themselves. See the file for the full `changelog-sections` list.

### `.release-please-manifest.json`

```json
{ ".": "0.0.0" }
```

The manifest starts at `0.0.0` because nothing has been released. The first release is pinned to v0.1.0 with a commit whose body carries the footer `Release-As: 0.1.0` (release-please reads it case-insensitively from the commit body of a commit on `main`; [documented here](https://github.com/googleapis/release-please#how-do-i-change-the-version-number)). If the first release pull request proposes a different version, set `"release-as": "0.1.0"` on the package in `release-please-config.json` ([manifest releaser](https://github.com/googleapis/release-please/blob/main/docs/manifest-releaser.md)) and remove it again after the release; otherwise later runs keep using that version.

### `.github/workflows/release.yml`

Two jobs on every push to `main`:

1. `release-please` runs `googleapis/release-please-action@v5` with the config and manifest above, and exposes `release_created` and `tag_name`. It needs `contents`, `issues`, and `pull-requests` write permissions (it maintains the release pull request and its labels).
2. `goreleaser` needs the first job and runs only when `release_created == 'true'`. It checks out the tag with full history, installs Go, and runs `goreleaser/goreleaser-action@v7` with `version: v2.19.0` and `args: release --clean`. It reads `GITHUB_TOKEN` and the secret `HOMEBREW_TAP_GITHUB_TOKEN`, and the optional secrets `AUR_KEY` and `NUR_GITHUB_TOKEN` (an unset secret reaches goreleaser as an empty variable).

A `concurrency` group serializes release runs. Pinned versions and why:

| Component | Pin | Evidence |
| --- | --- | --- |
| `googleapis/release-please-action` | `v5` (v5.0.0, 2026-04-22) | The only breaking change from v4 is the runtime upgrade to Node 24 ([#1188](https://github.com/googleapis/release-please-action/issues/1188)); the inputs `config-file` and `manifest-file` are unchanged in `action.yml`, and v5 added no required inputs |
| `goreleaser/goreleaser-action` | `v7` (v7.3.0, 2026-10-12) | v7.0.0 is the Node 24 and ESM upgrade (`feat!`); the inputs `distribution`, `version`, `args`, `workdir`, and `install-only` are unchanged (v7 added `version-file`) |
| goreleaser | `v2.19.0` (2026-10-22) | The latest stable release at implementation time. `brews` is deprecated since v2.10 (soft) and v2.16 (hard) in favor of `homebrew_casks`; the `hooks.post.install_steps` form used below is "since v2.19" in the [docs](https://goreleaser.com/customization/homebrew_casks/). An exact version keeps releases reproducible; bump it deliberately |
| `actions/checkout`, `actions/setup-go` | `v7` | The same majors as `ci.yml` |

### `.goreleaser.yaml`

- A `before` hook runs `go run ./internal/docgen -version {{ .Version }} -date {{ .CommitDate }} dist-extras`, which writes the completion scripts and the man page (see "Completions and man page" below).
- One build, `./cmd/spmeta`, for `darwin` and `linux` on `amd64` and `arm64`, with `CGO_ENABLED=0`, `-trimpath`, and the `-X` ldflags from "Version in the binary".
- `tar.gz` archives named `spmeta_<version>_<os>_<arch>` that contain the binary, `LICENSE`, `README.md`, `completions/spmeta.bash`, `completions/spmeta.zsh`, `completions/spmeta.fish`, and `manpages/spmeta.1.gz`, and one `checksums.txt`. `CHANGELOG.md` is not in the archives: goreleaser fails on a file pattern that matches nothing, and the file does not exist until the first release pull request merges, so `release-check` would fail until then.
- `changelog.disable: true` and `release.mode: keep-existing`.
- `nfpms` builds `.deb` and `.rpm` packages (maintainer `Bastian Kuberek <bastian@bkuberek.com>`, license MIT, binary in `/usr/bin`) that also install the completions and the man page.
- `homebrew_casks` pushes `Casks/spmeta.rb` to `bkuberek/homebrew-tap` with the token `HOMEBREW_TAP_GITHUB_TOKEN`. Its `manpages` and `completions` fields name the files in the archive. A post-install hook removes the quarantine flag on macOS:

  ```yaml
  hooks:
    post:
      install_steps: |
        on_macos do
          run "/usr/bin/xattr", args: ["-dr", "com.apple.quarantine", "{{ .StagedPath }}/spmeta"]
        end
  ```

  The goreleaser documentation warns that this bypasses macOS security and may stop working in a future macOS version; signing and notarization is the lasting fix.

The generated cask has `on_macos` and `on_linux` blocks, each with `on_arm` and `on_intel` archives, a `binary "spmeta"` stanza, `manpage`, `bash_completion`, `zsh_completion`, and `fish_completion` stanzas, and the hook as a `postflight_steps` block. Homebrew documents the `postflight_steps` DSL and requires it for casks in official taps; its docs do not state the Homebrew version that introduced it, so a very old Homebrew may reject the cask.

- `aurs` generates the `spmeta-bin` package (`PKGBUILD` and `.SRCINFO`) from the Linux archives for `x86_64` and `aarch64`, with `provides` and `conflicts` set to `spmeta`. Its `package()` function installs the binary, the license to `/usr/share/licenses/spmeta-bin/LICENSE`, the completions, and the man page, from the archive paths in the table under "Completions and man page". The push goes to `ssh://aur@aur.archlinux.org/spmeta-bin.git` with the key in `AUR_KEY`.
- `nix` generates `pkgs/spmeta/default.nix` for `x86_64` and `aarch64` on Linux and macOS and pushes it to `bkuberek/nur-packages` (branch `main`) with the token `NUR_GITHUB_TOKEN`. `extra_install` adds `installManPage ./manpages/spmeta.1.gz` and `post_install` adds `installShellCompletion ./completions/*`; goreleaser adds `installShellFiles` to `nativeBuildInputs` itself. goreleaser computes the hashes itself, so neither the pipe nor `release-check` needs the Nix tools.
- Both sections set `skip_upload` to the template `{{ if not (index .Env "AUR_KEY") }}true{{ end }}` (and the same with `NUR_GITHUB_TOKEN`). The template renders `true` when the variable is unset or empty and an empty string otherwise, so a release without the secret still generates the files in `dist/` and logs `pipe skipped or partially skipped reason=aur.skip_upload is set` (and `nix.skip_upload`) instead of failing. `index .Env "NAME"` is used because `.Env.NAME` fails on an unset variable.

### Completions and man page

`internal/reference` describes every command and flag once, and renders the `--help` pages, the completion scripts that `spmeta completion` prints, and the `spmeta(1)` man page from it. A test in `internal/cli` fails when a command registers a flag that the reference lacks, or the reverse.

`internal/docgen` writes the release copies into `dist-extras/`, which is ignored by git. The directory is separate from `dist/` because goreleaser checks that `dist/` is empty after the `before` hooks run. The man page header shows the version and the commit date, and the gzip header carries no name or time, so a rebuild of the same commit gives the same bytes.

| File | In the archive | `.deb` | `.rpm` | Homebrew stanza |
| --- | --- | --- | --- | --- |
| `spmeta.bash` | `completions/spmeta.bash` | `/usr/share/bash-completion/completions/spmeta` | same as `.deb` | `bash_completion` |
| `spmeta.zsh` | `completions/spmeta.zsh` | `/usr/share/zsh/vendor-completions/_spmeta` | `/usr/share/zsh/site-functions/_spmeta` | `zsh_completion` |
| `spmeta.fish` | `completions/spmeta.fish` | `/usr/share/fish/vendor_completions.d/spmeta.fish` | same as `.deb` | `fish_completion` |
| `spmeta.1.gz` | `manpages/spmeta.1.gz` | `/usr/share/man/man1/spmeta.1.gz` | same as `.deb` | `manpage` |

The zsh file has a different path per package because no directory is on the default `$fpath` of both families: Debian's zsh reads `/usr/share/zsh/vendor-completions` but not `/usr/share/zsh/site-functions`, and Fedora's reads `/usr/share/zsh/site-functions` but not `vendor-completions` (checked with `zsh -fc 'print -l $fpath'` in `debian:stable` and `fedora:latest`). nfpm's `packager` field selects the entry. The man page is installed gzip-compressed because Debian Policy asks for man pages compressed with `gzip -9n`, and nfpm installs files exactly as given. Homebrew renames the completion files on install (`spmeta.bash` to `spmeta`, `spmeta.zsh` to `_spmeta`) and accepts a `.gz` man page.

### `ci.yml` job `release-check`

On every push and pull request, `release-check` runs `goreleaser check` and then `goreleaser release --snapshot --clean --skip=publish` with the same pinned action and goreleaser version. It needs no secrets.

## Why a cask, and does it work on Linux

goreleaser's `brews` (formula) section is deprecated: soft since v2.10 and hard since v2.16, and the page says generating formulae for prebuilt binaries was the only Linuxbrew option "at the time, but this is no longer true, and _Casks_ should be used instead" ([goreleaser deprecations](https://goreleaser.com/deprecations/)). `spmeta` therefore uses `homebrew_casks`.

Casks install on Linux with a recent Homebrew. Homebrew 4.5.0 (2025-04-29) added "preliminary Linux support for casks": "Some Homebrew casks are supported on Linux. Right now these are mostly fonts and those with Linux binaries" ([Homebrew 4.5.0](https://brew.sh/2025/04/29/homebrew-4.5.0/)). The Cask Cookbook states that the `binary` artifact "works on either operating system" and that cross-platform casks scope OS-specific stanzas in `on_macos` or `on_linux` blocks ([Cask Cookbook](https://docs.brew.sh/Cask-Cookbook)). The goreleaser-generated cask follows that shape. "Preliminary" means Linux cask support may change; the `.deb`, `.rpm`, and archive channels do not depend on it. The Linux install of the cask has not been run yet; the post-release checklist covers it.

## Distribution channels

| Channel | Command for users | Produced by |
| --- | --- | --- |
| Homebrew (macOS, Linux) | `brew install --cask bkuberek/tap/spmeta` | goreleaser `homebrew_casks` |
| Debian, Ubuntu | `sudo apt install ./spmeta_0.1.0_linux_amd64.deb` | goreleaser `nfpms` |
| Fedora, RHEL | `sudo dnf install ./spmeta_0.1.0_linux_amd64.rpm` | goreleaser `nfpms` |
| Archive | Download from GitHub Releases, verify with `checksums.txt` | goreleaser `archives` |
| Arch Linux (AUR) | `yay -S spmeta-bin` or `paru -S spmeta-bin` | goreleaser `aurs`, once `AUR_KEY` is set |
| Nix (NUR) | `pkgs.nur.repos.bkuberek.spmeta` | goreleaser `nix`, once `NUR_GITHUB_TOKEN` is set and the repository is registered with NUR |
| Go | `go install github.com/bkuberek/spmeta/cmd/spmeta@latest` | The tag |

macOS binaries are not signed or notarized in v1. Homebrew installs run the quarantine-removal hook, and `go install` builds locally, so neither is quarantined. A manually downloaded archive needs `xattr -d com.apple.quarantine ./spmeta` before the first run; the README says so.

## Repository setup

These one-time steps are done. No secret values belong in the repository or in these documents.

1. `github.com/bkuberek/spmeta` is published, with the MIT license and the maintainer email `bastian@bkuberek.com` for Linux packages.
2. `github.com/bkuberek/homebrew-tap` exists and is public. goreleaser creates the `Casks/` directory on the first push; the placeholder `Formula/.gitkeep` is harmless.
3. The Actions secret `HOMEBREW_TAP_GITHUB_TOKEN` in `bkuberek/spmeta` holds a fine-grained personal access token with "Contents: read and write" on `bkuberek/homebrew-tap` only.
4. "Allow GitHub Actions to create and approve pull requests" is enabled, and only "Rebase and merge" is allowed.
5. `main` is protected with these required checks, which are the full job names of the matrix: `test (ubuntu-latest, go stable)`, `test (ubuntu-latest, go oldstable)`, `test (macos-latest, go stable)`, `test (macos-latest, go oldstable)`, `lint`, `integration`, and `commits`. `release-check` is not required.

## AUR and Nix one-time setup

The AUR and Nix channels are configured in `.goreleaser.yaml` but publish nothing until the accounts and secrets below exist. Until then every release succeeds and skips both uploads. Each secret enables its own channel, and nothing in the repository changes when you add them. These steps are done by the maintainer outside the repository; no secret values belong in the repository or in these documents.

### AUR (`spmeta-bin`)

1. Create an account at <https://aur.archlinux.org/register>.
2. Generate a dedicated key pair without a passphrase, because goreleaser cannot unlock a protected key:

   ```bash
   ssh-keygen -t ed25519 -N "" -C "spmeta goreleaser" -f ~/.ssh/aur_spmeta
   ```

3. In the AUR account settings, paste the contents of `~/.ssh/aur_spmeta.pub` into "SSH Public Key".
4. Store the private key as the Actions secret `AUR_KEY` of `bkuberek/spmeta`:

   ```bash
   gh secret set AUR_KEY --repo bkuberek/spmeta < ~/.ssh/aur_spmeta
   ```

5. Optional: confirm the key authenticates with `ssh -i ~/.ssh/aur_spmeta aur@aur.archlinux.org help`.

The AUR creates a package when you push to a new `ssh://aur@aur.archlinux.org/<name>.git` URL. If the first release fails with an SSH or permission error, the account or the public key is the cause. goreleaser v2.19.0 publishes in the order GitHub Release, Nix, cask, AUR, and continues past a Nix, cask, or AUR error, so by then the GitHub Release and the cask are already published; the workflow run is red only because of the failed push. Fix the key; the next release publishes the package. Re-running the failed `goreleaser` job repeats the whole publish, including the asset upload to the existing release, which has not been tested.

### Nix (NUR)

1. Create the public repository `bkuberek/nur-packages` from the [NUR package template](https://github.com/nix-community/nur-packages-template) (click "Use this template", or run the command below). The default branch must be `main`.

   ```bash
   gh repo create bkuberek/nur-packages --public --template nix-community/nur-packages-template
   ```

2. Create a fine-grained personal access token for `bkuberek/nur-packages` only, with the permission "Contents: Read and write", and store it as the Actions secret `NUR_GITHUB_TOKEN` of `bkuberek/spmeta`:

   ```bash
   gh secret set NUR_GITHUB_TOKEN --repo bkuberek/spmeta
   ```

   `gh` prompts for the value, which keeps it out of your shell history.
3. Register the repository with NUR: open a pull request against [nix-community/NUR](https://github.com/nix-community/NUR) that adds this entry to `repos.json`, and follow the NUR README for the checks it runs:

   ```json
   "bkuberek": {
     "github-contact": "bkuberek",
     "url": "https://github.com/bkuberek/nur-packages"
   }
   ```

4. Check that the template's `default.nix` picks up `pkgs/` packages, or add `spmeta = pkgs.callPackage ./pkgs/spmeta { };` to the repository's `default.nix`. The release writes only `pkgs/spmeta/default.nix`.

The channels take effect on the next release after the secrets exist; goreleaser does not republish an earlier release. After that release, run `yay -S spmeta-bin` on an Arch machine or container and `nix run github:nix-community/NUR#repos.bkuberek.spmeta -- version`, and record the results in the post-release checklist.

## Release checklist

Before merging a release pull request:

1. Confirm CI is green on `main`.
2. Confirm the manual matrix rows for the shipped backends are recorded ([testing.md](./testing.md)).
3. Read the release pull request's `CHANGELOG.md` section; if a change is missing or misclassified, fix it with a follow-up commit (for example a `fix:` commit), not by editing the changelog.
4. Confirm the version bump matches the table in Versioning, in particular that breaking changes before 1.0.0 bump the minor version.
5. Merge the release pull request with `gh pr merge --admin --rebase <number>`; its required checks never run (see Release flow).
6. For the first release, confirm the pull request proposes v0.1.0 (the `Release-As: 0.1.0` footer).

After the release workflow finishes:

1. Confirm the GitHub Release lists four archives, `checksums.txt`, two `.deb`, and two `.rpm` files.
2. Run `brew install --cask bkuberek/tap/spmeta && spmeta version` on macOS and confirm the version, that `man spmeta` opens, and that `spmeta --<Tab>` completes in your shell; on a Linux machine with a current Homebrew, run the same command and record the result.
3. Run `go install github.com/bkuberek/spmeta/cmd/spmeta@vX.Y.Z` and confirm `spmeta version`.
4. Install the `.deb` on an Ubuntu machine or container and run `spmeta version` and `man -w spmeta`.
5. When the AUR and Nix secrets are configured, confirm the GitHub Actions log shows the AUR and Nix pushes instead of `skip_upload is set`, and that `spmeta-bin` appears on <https://aur.archlinux.org/packages/spmeta-bin>.
