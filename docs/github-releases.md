# GitHub Release Procedure

This document defines the release procedure and packaging conventions for the public `Gen3Games/axiom-cli` repository.

## Release conventions

- Repository: `Gen3Games/axiom-cli`
- Tag format: `vX.Y.Z`
- Release title: `Axiom CLI vX.Y.Z`
- Asset filename prefix: `axiom-vX.Y.Z`
- Binary name inside archives:
  - `axiom` on macOS and Linux
  - `axiom.exe` on Windows
- Archive formats:
  - macOS: `.tar.gz`
  - Linux: `.tar.gz`
  - Windows: `.zip`
- Checksum asset: `axiom-vX.Y.Z-checksums.txt`

The repository name stays `axiom-cli`, but published binaries and release assets use the shorter `axiom` name.

## Published asset matrix

Each release must include exactly these assets:

- `axiom-vX.Y.Z-darwin-amd64.tar.gz`
- `axiom-vX.Y.Z-darwin-arm64.tar.gz`
- `axiom-vX.Y.Z-linux-amd64.tar.gz`
- `axiom-vX.Y.Z-linux-arm64.tar.gz`
- `axiom-vX.Y.Z-windows-amd64.zip`
- `axiom-vX.Y.Z-windows-arm64.zip`
- `axiom-vX.Y.Z-checksums.txt`

## Prerequisites

- GitHub CLI authenticated with access to `Gen3Games/axiom-cli`
- Go 1.24+
- `zip`, `tar`, and `sha256sum` available on the system

On Ubuntu or Debian, install `zip` with:

```bash
sudo apt-get update
sudo apt-get install -y zip
```

Verify authentication before releasing:

```bash
gh auth status
```

## Release steps

Run all commands from the `axiom-cli/` repository root.

### 1. Validate the repository state

```bash
git fetch origin main
git pull --ff-only origin main
git status --short
git branch --show-current
go test ./...
```

Release from the latest intended commit on `main` unless there is a reason to publish a different target.

Before building, confirm that:

- local `main` is fast-forwarded to `origin/main`
- `git status --short` only shows intentional local changes
- you are not packaging stale local source behind the published branch head

### 2. Build release artifacts

Set the version you are releasing:

```bash
version=v0.1.0
```

Build and package the full matrix:

```bash
rm -rf dist
mkdir -p dist

for target in darwin/amd64 darwin/arm64 linux/amd64 linux/arm64 windows/amd64 windows/arm64; do
  GOOS=${target%/*}
  GOARCH=${target#*/}
  base="axiom-${version}-${GOOS}-${GOARCH}"
  dir="dist/${base}"
  bin="axiom"

  mkdir -p "$dir"

  if [ "$GOOS" = "windows" ]; then
    bin="${bin}.exe"
  fi

  env CGO_ENABLED=0 GOOS="$GOOS" GOARCH="$GOARCH" \
    go build -trimpath -ldflags='-s -w' -o "$dir/$bin" ./cmd/axiom

  cp README.md "$dir/README.md"

  if [ "$GOOS" = "windows" ]; then
    (cd dist && zip -qr "${base}.zip" "$base")
  else
    tar -C dist -czf "dist/${base}.tar.gz" "$base"
  fi
done

sha256sum \
  "dist/axiom-${version}-darwin-amd64.tar.gz" \
  "dist/axiom-${version}-darwin-arm64.tar.gz" \
  "dist/axiom-${version}-linux-amd64.tar.gz" \
  "dist/axiom-${version}-linux-arm64.tar.gz" \
  "dist/axiom-${version}-windows-amd64.zip" \
  "dist/axiom-${version}-windows-arm64.zip" \
  > "dist/axiom-${version}-checksums.txt"
```

### 3. Verify local artifacts

```bash
ls -1 dist
cat "dist/axiom-${version}-checksums.txt"
```

Confirm that:

- all six platform archives exist
- the checksum file exists
- Windows assets are `.zip`
- the binary name inside each archive is `axiom` or `axiom.exe`

### 4. Prepare release notes

Create a notes file first.

Example:

```markdown
## Axiom CLI v0.1.0

First public release of the standalone Axiom CLI for XRPL EVM users.

Included in this release:
- Local wallet creation and import for XRPL EVM
- Optional native XRPL wallet support for bridge submissions
- Backend registration flow for destination tags
- Market discovery, profile reads, and funding metadata
- On-chain prediction placement and claim flows
- Prebuilt binaries for macOS, Linux, and Windows on amd64 and arm64

Assets:
- `axiom-v0.1.0-darwin-amd64.tar.gz`
- `axiom-v0.1.0-darwin-arm64.tar.gz`
- `axiom-v0.1.0-linux-amd64.tar.gz`
- `axiom-v0.1.0-linux-arm64.tar.gz`
- `axiom-v0.1.0-windows-amd64.zip`
- `axiom-v0.1.0-windows-arm64.zip`
- `axiom-v0.1.0-checksums.txt`

Each archive contains the executable named `axiom` or `axiom.exe` on Windows.

Verify downloads with the published SHA-256 checksum file.
```

### 5. Create the GitHub release

```bash
gh release create "$version" \
  "dist/axiom-${version}-darwin-amd64.tar.gz" \
  "dist/axiom-${version}-darwin-arm64.tar.gz" \
  "dist/axiom-${version}-linux-amd64.tar.gz" \
  "dist/axiom-${version}-linux-arm64.tar.gz" \
  "dist/axiom-${version}-windows-amd64.zip" \
  "dist/axiom-${version}-windows-arm64.zip" \
  "dist/axiom-${version}-checksums.txt" \
  --repo Gen3Games/axiom-cli \
  --target main \
  --title "Axiom CLI ${version}" \
  --notes-file /tmp/axiom-release-notes.md
```

If GitHub creates the release as a draft, publish it explicitly:

```bash
gh release edit "$version" --repo Gen3Games/axiom-cli --draft=false
```

### 6. Verify the published release

```bash
gh release view "$version" --repo Gen3Games/axiom-cli
gh release view "$version" --repo Gen3Games/axiom-cli --json url,isDraft,tagName,assets
```

Confirm that:

- `isDraft` is `false`
- the tag is correct
- only the expected asset names are present
- the release notes match the asset names
- the release tag resolves to the same commit the assets were built from

Check the local and remote tag target explicitly:

```bash
git rev-parse HEAD
git rev-parse "$version"
git ls-remote --tags origin "$version"
```

If you had to fix release-blocking issues locally before publishing, commit and push those fixes first, then move the tag to the committed source before considering the release complete.

## Correcting an existing release

If you need to replace assets on an existing release:

Upload corrected assets:

```bash
gh release upload "$version" dist/<asset-name> --repo Gen3Games/axiom-cli
```

Replace an existing asset in place:

```bash
gh release upload "$version" dist/<asset-name> --repo Gen3Games/axiom-cli --clobber
```

Delete an obsolete asset:

```bash
gh release delete-asset "$version" <asset-name> --repo Gen3Games/axiom-cli --yes
```

Update release notes:

```bash
gh release edit "$version" --repo Gen3Games/axiom-cli --notes-file /tmp/axiom-release-notes.md
```

## Final checklist

- `git pull --ff-only origin main` was run before the build
- `go test ./...` passes
- release tag uses `vX.Y.Z`
- release tag resolves to the same source commit used to build the assets
- asset names use hyphens, not underscores
- asset names use `axiom`, not `axiom-cli`
- Windows assets are `.zip`
- checksum file matches the final uploaded assets
- release is public, not draft
- release notes match the final published asset names