# Changelog

## [0.3.0](https://github.com/bkuberek/spmeta/compare/v0.2.0...v0.3.0) (2026-10-31)


### Features

* **cli:** add shell completions and a man page from one command reference ([2d0976d](https://github.com/bkuberek/spmeta/commit/2d0976d453712e43697f4894cfcf907290a4fbbf))
* **format:** add the waybar output format and status-bar examples ([1f6e8c1](https://github.com/bkuberek/spmeta/commit/1f6e8c1396b269a45bffd8a2e82da7b7a81f15be))
* **macos:** name the app macOS asks about for any terminal or status bar ([5812cc7](https://github.com/bkuberek/spmeta/commit/5812cc71d4347e4452fecc43bd39d82085350fe6))
* **watch:** read at once when an MPRIS player signals a change on Linux ([cbca5bb](https://github.com/bkuberek/spmeta/commit/cbca5bbca0ae3411ac0995c2314737fe83cf7ebf))
* **watch:** read at once when Spotify signals a change on macOS ([bbc8a07](https://github.com/bkuberek/spmeta/commit/bbc8a07b10246f90c936c8a5b15d9e98818d10df))


### Bug Fixes

* **doctor:** check the Web login even when the config file is invalid ([df33cd5](https://github.com/bkuberek/spmeta/commit/df33cd546a35349d60ed616d21ca56787dab7e5e))

## [0.2.0](https://github.com/bkuberek/spmeta/compare/v0.1.0...v0.2.0) (2026-10-29)


### Features

* **auth:** add loopback login and the spmeta auth commands ([8f1e174](https://github.com/bkuberek/spmeta/commit/8f1e174a506164361915bb147070c0e7198be481))
* **auth:** add PKCE, token file and locked refresh for Web mode ([188a15b](https://github.com/bkuberek/spmeta/commit/188a15b54e7a398b47bc1c407250a4ec19cb4605))
* **auth:** default the login redirect to port 8974 ([9046a7d](https://github.com/bkuberek/spmeta/commit/9046a7d023a34316f98c425c30647a11abd5f7c2))
* **web:** read playback state from the Spotify Web API ([bea50fb](https://github.com/bkuberek/spmeta/commit/bea50fbbb925bea0ec903c958bb35128901d0ee7))
* **web:** use the Web API in auto, --source web, watch and doctor ([292bfc5](https://github.com/bkuberek/spmeta/commit/292bfc52177e7b13d37240a8e5808ee0843341eb))

## 0.1.0 (2026-10-29)


### Features

* **cli:** scaffold spmeta CLI with version, help and CI ([24bc395](https://github.com/bkuberek/spmeta/commit/24bc3956c0df55917f52ff6d7100f13ca467bdc4))
* **config:** resolve settings from flags, environment and config file ([3433a1a](https://github.com/bkuberek/spmeta/commit/3433a1aa9f4abc143ccc3a6ca7c5b4fc96c56441))
* **doctor:** add spmeta doctor and the user README ([7a280c6](https://github.com/bkuberek/spmeta/commit/7a280c6084b1be9e41c72b4f76b29b5bf7c570b2))
* **format:** add text, JSON and template output with read path wiring ([87217ab](https://github.com/bkuberek/spmeta/commit/87217ab9ea194f474a0e36110bb422260403df11))
* **macos:** read Spotify.app via guarded AppleScript ([16cc709](https://github.com/bkuberek/spmeta/commit/16cc709347a7557a742c1c672b4484db06ff27aa))
* **model:** add NowPlaying model, typed errors and JSON Schema ([ae2104c](https://github.com/bkuberek/spmeta/commit/ae2104c49e1cd65702c480942feef7d9c8bd392e))
* **mpris:** add godbus session-bus adapter and integration test ([f52e575](https://github.com/bkuberek/spmeta/commit/f52e575ba82e033eb7dd7acb35e4a3c62cbb273c))
* **mpris:** select Linux MPRIS players and map their metadata ([ab24106](https://github.com/bkuberek/spmeta/commit/ab24106ffef2caf1f9fb3397de3a65ffc84406c3))
* **resolve:** select the source per ADR 0005 and wire local backends ([48cd41e](https://github.com/bkuberek/spmeta/commit/48cd41e1388083e928677a10082acd2442b2e0f5))
* **watch:** add watch mode with change detection and NDJSON output ([e7992ff](https://github.com/bkuberek/spmeta/commit/e7992ffdc6d6abf2d84315221ea7eccdaa18a28a))


### CI

* add release automation with release-please and goreleaser ([4935507](https://github.com/bkuberek/spmeta/commit/4935507e6afcce20da69e25c56ff15b4de98da3b))
