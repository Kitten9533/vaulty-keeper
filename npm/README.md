# vaulty-keeper (npm)

Single Go binary for encrypted-at-rest snapshots, AES helpers and local database
tunnels, with a loopback-only web UI (`vaulty-keeper ui`). This package embeds
the prebuilt platform binaries from the [GitHub releases](https://github.com/Kitten9533/vaulty-keeper/releases);
there is no postinstall step and nothing is downloaded at runtime.

Installing through npm avoids the unsigned-binary warnings: npm fetches the
package over HTTPS, so macOS Gatekeeper and Windows SmartScreen do not apply
their browser-download markers.

## Install

```sh
npm install -g vaulty-keeper
```

Node.js 18+ is required. The binary is fetched by the package manager, not a
browser, so first run needs no right-click-Open or Unblock-File workaround.

## First run

```sh
vaulty-keeper apollo init      # create the snapshot key (OS keychain)
vaulty-keeper sensitive init   # create the sensitive-value key
vaulty-keeper ui               # local web UI (loopback only)
```

See the [README](https://github.com/Kitten9533/vaulty-keeper#readme) for full
usage and the security model.

## Maintainers

Version must match the GitHub release tag. Release flow:

```sh
make release                     # builds release/ archives + sha256sums.txt
node npm/scripts/build.mjs       # extracts binaries into npm/platform/, syncs version
npm pack                         # inspect the tarball (size, contents)
npm publish                      # from npm/
```

`npm/platform/` is generated and git-ignored; commit only `package.json`, `bin/`
and `scripts/`.
