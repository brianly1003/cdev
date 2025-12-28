# Releasing cdev

This document explains how to release new versions of cdev.

## Quick Start Checklist (First-Time Setup)

Follow these steps in order to set up releases for the first time:

### Step 1: Commit Release Configuration Files

```bash
git add .goreleaser.yaml .github/ docs/RELEASING.md
git commit -m "ci: add GoReleaser and GitHub Actions for releases"
git push
```

### Step 2: Create Homebrew Tap Repository

1. Go to https://github.com/new
2. Repository name: `homebrew-tap` (must be public)
3. Add description: "Homebrew formulas for cdev"
4. Click "Create repository"
5. Create a `Formula` folder:
   ```bash
   # Clone your new repo
   git clone https://github.com/brianly1003/homebrew-tap.git
   cd homebrew-tap

   # Create Formula directory and README
   mkdir Formula

   cat > README.md << 'EOF'
   # Homebrew Tap

   Custom Homebrew formulas for my projects.

   ## Installation

   ```bash
   brew tap brianly1003/tap
   brew install cdev
   ```
   EOF

   git add .
   git commit -m "Initial setup"
   git push
   ```

### Step 3: Create Personal Access Token

1. Go to https://github.com/settings/tokens
2. Click **"Generate new token"** → **"Generate new token (classic)"**
3. Note: `homebrew-tap-token`
4. Expiration: Choose based on your preference (e.g., "No expiration" or "1 year")
5. Select scope: **`repo`** (full control of private repositories)
6. Click **"Generate token"**
7. **Copy the token immediately** (you won't see it again!)

### Step 4: Add Secret to cdev Repository

1. Go to https://github.com/brianly1003/cdev/settings/secrets/actions
2. Click **"New repository secret"**
3. Name: `HOMEBREW_TAP_GITHUB_TOKEN`
4. Secret: Paste the token from Step 3
5. Click **"Add secret"**

### Step 5: Create Your First Release

```bash
# Make sure you're on main branch with latest changes
git checkout main
git pull

# Create an annotated tag
git tag -a v1.0.0 -m "Release v1.0.0"

# Push the tag (this triggers the release workflow)
git push origin v1.0.0
```

### Step 6: Monitor the Release

1. Go to https://github.com/brianly1003/cdev/actions
2. Watch the "Release" workflow
3. Once complete (green checkmark), verify:
   - Releases page: https://github.com/brianly1003/cdev/releases
   - Homebrew tap: https://github.com/brianly1003/homebrew-tap/tree/main/Formula

### Step 7: Test Installation

```bash
# Test Homebrew installation
brew tap brianly1003/tap
brew install cdev
cdev version

# Or test Go installation
go install github.com/brianly1003/cdev/cmd/cdev@v1.0.0
```

---

## Subsequent Releases

After the initial setup, releasing a new version is simple:

```bash
# 1. Ensure all changes are committed and pushed
git status
git push

# 2. Create and push a new tag
git tag -a v1.1.0 -m "Release v1.1.0"
git push origin v1.1.0

# 3. Monitor at https://github.com/brianly1003/cdev/actions
```

---

## Detailed Documentation

### Prerequisites

1. **GoReleaser** (optional, for local testing):
   ```bash
   brew install goreleaser
   # or
   go install github.com/goreleaser/goreleaser/v2@latest
   ```

2. **GitHub Secrets** (required for automated releases):
   - `GITHUB_TOKEN` - Automatically provided by GitHub Actions
   - `HOMEBREW_TAP_GITHUB_TOKEN` - Personal Access Token with `repo` scope

### Manual Release (Local)

For testing or manual releases without GitHub Actions:

```bash
# Dry run (no publish, good for testing)
goreleaser release --snapshot --clean

# Full release (requires tokens)
export GITHUB_TOKEN="your-github-token"
export HOMEBREW_TAP_GITHUB_TOKEN="your-homebrew-tap-token"
goreleaser release --clean
```

### Version Numbering

We follow [Semantic Versioning](https://semver.org/):

| Change Type | Example | When to Use |
|-------------|---------|-------------|
| **MAJOR** | v1.0.0 → v2.0.0 | Breaking changes |
| **MINOR** | v1.0.0 → v1.1.0 | New features (backwards compatible) |
| **PATCH** | v1.0.0 → v1.0.1 | Bug fixes (backwards compatible) |

Pre-release versions:
- `v1.0.0-alpha.1` - Alpha release (early testing)
- `v1.0.0-beta.1` - Beta release (feature complete, testing)
- `v1.0.0-rc.1` - Release candidate (final testing)

### What Gets Published

Each release automatically creates:

1. **GitHub Release** with:
   - Binary archives for all platforms (tar.gz/zip)
   - SHA256 checksums
   - Auto-generated changelog from commit messages

2. **Homebrew Formula** pushed to `brianly1003/homebrew-tap`

3. **Supported Platforms**:
   | OS | Architecture | File |
   |----|--------------|------|
   | macOS | Apple Silicon (M1/M2/M3) | `cdev_*_darwin_arm64.tar.gz` |
   | macOS | Intel | `cdev_*_darwin_amd64.tar.gz` |
   | Linux | x86_64 | `cdev_*_linux_amd64.tar.gz` |
   | Linux | ARM64 | `cdev_*_linux_arm64.tar.gz` |
   | Windows | x86_64 | `cdev_*_windows_amd64.zip` |

### Installing Released Versions

#### Homebrew (macOS/Linux)

```bash
# Add the tap (first time only)
brew tap brianly1003/tap

# Install
brew install cdev

# Upgrade to latest
brew upgrade cdev

# Check version
cdev version
```

#### Go Install

```bash
# Latest version
go install github.com/brianly1003/cdev/cmd/cdev@latest

# Specific version
go install github.com/brianly1003/cdev/cmd/cdev@v1.0.0
```

#### Manual Download

Download from [GitHub Releases](https://github.com/brianly1003/cdev/releases):

```bash
# macOS (Apple Silicon)
curl -LO https://github.com/brianly1003/cdev/releases/download/v1.0.0/cdev_1.0.0_darwin_arm64.tar.gz
tar -xzf cdev_1.0.0_darwin_arm64.tar.gz
sudo mv cdev /usr/local/bin/
cdev version

# macOS (Intel)
curl -LO https://github.com/brianly1003/cdev/releases/download/v1.0.0/cdev_1.0.0_darwin_amd64.tar.gz
tar -xzf cdev_1.0.0_darwin_amd64.tar.gz
sudo mv cdev /usr/local/bin/

# Linux (x86_64)
curl -LO https://github.com/brianly1003/cdev/releases/download/v1.0.0/cdev_1.0.0_linux_amd64.tar.gz
tar -xzf cdev_1.0.0_linux_amd64.tar.gz
sudo mv cdev /usr/local/bin/
```

---

## Troubleshooting

### GoReleaser fails with "tag not found"

Ensure you've pushed the tag:
```bash
git tag -l  # List local tags
git push origin v1.0.0  # Push specific tag
git push --tags  # Push all tags
```

### Homebrew formula not updating

1. Check if `HOMEBREW_TAP_GITHUB_TOKEN` secret exists in repository settings
2. Verify the token has `repo` scope
3. Check the homebrew-tap repository for the Formula file

### Release workflow fails

1. Go to Actions tab and click on the failed workflow
2. Expand the failed step to see error details
3. Common issues:
   - Missing `HOMEBREW_TAP_GITHUB_TOKEN` secret
   - Token expired or revoked
   - homebrew-tap repository doesn't exist

### Release already exists

GoReleaser won't overwrite existing releases. Options:
1. Delete the existing release on GitHub and re-run the workflow
2. Create a new tag with incremented version (e.g., v1.0.1)

### "Permission denied" when pushing to homebrew-tap

The `HOMEBREW_TAP_GITHUB_TOKEN` needs `repo` scope. Generate a new token with correct permissions.

---

## File Reference

| File | Purpose |
|------|---------|
| `.goreleaser.yaml` | GoReleaser configuration (builds, archives, homebrew) |
| `.github/workflows/release.yml` | GitHub Actions workflow triggered on tag push |
| `.github/workflows/ci.yml` | CI workflow for tests and linting on PRs |
