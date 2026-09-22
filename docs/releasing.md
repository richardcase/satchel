# Releasing

This is the maintainer runbook. It covers how a release is cut, and every
manual, out-of-repo step the release pipeline depends on: the secrets, the
repositories it pushes to, and the key that signs the Arch packages. If a step
is not written down here, the pipeline should not depend on it.

## Cutting a release

Push a semver tag:

```bash
git tag v0.12.0
git push origin v0.12.0
```

That runs `.github/workflows/release.yml`:

1. **Validate semver tag.** Anything that is not `vMAJOR.MINOR.PATCH`, with an
   optional prerelease or build suffix, fails here.
2. **`release`.** goreleaser builds the binaries, archives, and the `.deb`, `.rpm`
   and `.pkg.tar.zst` packages, then publishes the GitHub release. It also
   pushes the cask to `richardcase/homebrew-tap`, and uploads the
   `.pkg.tar.zst` files as a workflow artifact for the next job.
3. **`archlinux-repo`.** This runs in an `archlinux:base` container. It signs
   the packages, adds them to the pacman repository with `repo-add`, and pushes
   the result to `richardcase/archlinux-repo`, which GitHub Pages serves.

   **Prerelease tags (`v1.2.0-rc.1`) skip this job.** nfpm drops the suffix
   from pkgver, so a release candidate would be published as `1.2.0` and
   shadow the real release.

The changelog comes from Conventional Commit subjects. See
[AGENTS.md](../AGENTS.md#commit-messages).

## Secrets

All secrets live in **richardcase/satchel → Settings → Secrets and variables →
Actions**.

| Secret | What it is | Scope | Used by |
| --- | --- | --- | --- |
| `GITHUB_TOKEN` | Provided by Actions | `contents: write` on this repo (set in the workflow) | `release` |
| `HOMEBREW_TAP_TOKEN` | Fine-grained PAT | Contents: read and write on `richardcase/homebrew-tap` only | `release` |
| `ARCH_REPO_TOKEN` | Fine-grained PAT | Contents: read and write on `richardcase/archlinux-repo` only | `archlinux-repo` |
| `ARCH_REPO_GPG_PRIVATE_KEY` | ASCII-armoured secret key, no passphrase | Signs the packages and the repository database | `archlinux-repo` |

Create PATs at <https://github.com/settings/personal-access-tokens/new>. Give
each one access to a single repository and give it only the **Contents**
permission. Set them with:

```bash
gh secret set HOMEBREW_TAP_TOKEN --repo richardcase/satchel
gh secret set ARCH_REPO_TOKEN   --repo richardcase/satchel
```

## Arch Linux repository

The AUR is not accepting new packages, so satchel runs its own pacman
repository:

- **Source:** <https://github.com/richardcase/archlinux-repo>
- **Served at:** `https://richardcase.github.io/archlinux-repo/$arch`
- **Repository name:** `[richardcase]`. This is the `<repo-name>` argument to
  `.github/scripts/publish-archlinux-repo.sh`, so the database is
  `richardcase.db`.

Layout, which the publish script maintains:

```
archlinux-repo/
├── README.md             user-facing install instructions
├── .nojekyll             serve files as-is; no Jekyll build
├── richardcase.asc       public signing key
├── x86_64/
│   ├── richardcase.db        (+ .sig, .files, .files.sig, .tar.gz copies)
│   └── satchel-0.12.0-1-x86_64.pkg.tar.zst (+ .sig)
└── aarch64/              same, for Arch Linux ARM
```

The `.db` and `.files` entries are real copies rather than the symlinks
`repo-add` creates, because Pages does not reliably serve symlinks. Only the
current version of each package is kept, because `repo-add --remove` deletes
the superseded one.

### One-time bootstrap

You only need this once, or again if the repository is ever recreated.

**1. Create the signing key.** Use a dedicated key rather than a personal one:
it has no passphrase, because CI has to use it unattended.

```bash
export GNUPGHOME=$(mktemp -d)
gpg --batch --passphrase '' --quick-gen-key \
  "satchel packages <richmcase@gmail.com>" ed25519 sign 2y
FPR=$(gpg --with-colons --list-keys | awk -F: '$1 == "fpr" { print $10; exit }')
echo "$FPR"                                     # record this
gpg --armor --export "$FPR" > richardcase.asc   # public, committed
gpg --armor --export-secret-keys "$FPR" \
  | gh secret set ARCH_REPO_GPG_PRIVATE_KEY --repo richardcase/satchel
```

Keep an offline backup of the secret key, for example in a password manager:
`gpg --armor --export-secret-keys "$FPR"`. Without it, the expiry can't be
extended, and a new key would have to be rolled out to every user. When the
backup is safe, delete `$GNUPGHOME`.

**2. Create the repository.**

```bash
gh repo create richardcase/archlinux-repo --public \
  --description "pacman repository for richardcase's tools"
git clone git@github.com:richardcase/archlinux-repo.git && cd archlinux-repo
mkdir x86_64 aarch64 && touch .nojekyll x86_64/.gitkeep aarch64/.gitkeep
cp /path/to/richardcase.asc .
# write README.md from the template below, filling in the fingerprint
git add -A && git commit -m "chore: bootstrap pacman repository" && git push
```

**3. Enable Pages:** Settings → Pages → Build and deployment → *Deploy from a
branch*, branch `main`, folder `/ (root)`. Or run:

```bash
gh api -X POST repos/richardcase/archlinux-repo/pages \
  -f 'source[branch]=main' -f 'source[path]=/'
```

**4. Create `ARCH_REPO_TOKEN`** as described in [Secrets](#secrets).

**5. Publish.** Either tag a release, or re-run the `archlinux-repo` job of the
latest release from the Actions tab. Then check that it is served:

```bash
curl -fsI https://richardcase.github.io/archlinux-repo/x86_64/richardcase.db
```

#### `README.md` template for archlinux-repo

The archlinux-repo README is for users only. Maintainer steps stay here, so
there is one source of truth.

````markdown
# archlinux-repo

A signed pacman repository for [satchel](https://github.com/richardcase/satchel).

## Use

Trust the signing key (fingerprint `<FINGERPRINT>`):

```bash
curl -fsSL https://richardcase.github.io/archlinux-repo/richardcase.asc | sudo pacman-key --add -
sudo pacman-key --lsign-key <FINGERPRINT>
```

Add the repository to `/etc/pacman.conf`:

```ini
[richardcase]
Server = https://richardcase.github.io/archlinux-repo/$arch
```

Then run `sudo pacman -Sy satchel`. Later releases arrive with `pacman -Syu`.

This repository is written by satchel's release workflow. Don't commit to it
by hand. Maintainer notes are in
[satchel's docs/releasing.md](https://github.com/richardcase/satchel/blob/main/docs/releasing.md).
````

### Extending the key's expiry

The key is created with a two-year expiry. Before it lapses, extend it using
the offline backup:

```bash
export GNUPGHOME=$(mktemp -d)
gpg --import backup-secret.asc
gpg --quick-set-expire "$FPR" 2y
gpg --armor --export "$FPR" > richardcase.asc   # commit to archlinux-repo
gpg --armor --export-secret-keys "$FPR" \
  | gh secret set ARCH_REPO_GPG_PRIVATE_KEY --repo richardcase/satchel
```

The fingerprint stays the same, so users only need to refresh the key:
`curl -fsSL .../richardcase.asc | sudo pacman-key --add -`. Mention this in the
release notes.

### Rotating the key (lost or compromised)

1. If the old key is compromised, revoke it:
   `gpg --gen-revoke "$OLD_FPR" > revoke.asc && gpg --import revoke.asc`. Then
   commit the revoked public key to archlinux-repo as `richardcase-old.asc`.
2. Create a new key with the bootstrap step 1 commands, and replace the
   `ARCH_REPO_GPG_PRIVATE_KEY` secret.
3. Commit the new `richardcase.asc`, and update the fingerprint in the
   archlinux-repo README.
4. Re-sign everything by re-running the `archlinux-repo` job for the current
   release. That re-signs the package it adds and the database. Older packages
   are not kept, so there is nothing else to re-sign.
5. Tell users, in the release notes and the satchel README, to run
   `pacman-key --add` and `--lsign-key` for the new fingerprint. If the old
   key was compromised, they should also run
   `pacman-key --delete <old fingerprint>`.

### Renewing a token

Fine-grained PATs expire. An expired `ARCH_REPO_TOKEN` fails the
`archlinux-repo` job at the checkout or push step with a 401 or 403. An expired
`HOMEBREW_TAP_TOKEN` fails goreleaser's cask step. Regenerate the token with
the same single-repository scope, run `gh secret set`, and re-run the failed
job.

### Recovering a failed publish

Re-run the `archlinux-repo` job from the Actions tab, since the packages are
still available as the workflow artifact. The publish script is idempotent:
re-adding the same version replaces its database entry and keeps the file.

If the artifact has expired, which happens after 90 days by default, download
the `.pkg.tar.zst` files from the GitHub release instead. Then run the script
locally, as shown below, against a clone of archlinux-repo, and push.

### Testing the publish script locally

Running the script needs `repo-add` and `bsdtar`, which means Arch or an
`archlinux:base` container. Testing a signed install without root also needs
`fakeroot`.

```bash
make snapshot                             # dist/*.pkg.tar.zst
export GNUPGHOME=$(mktemp -d)
gpg --batch --passphrase '' --quick-gen-key "test <test@example.com>" ed25519 sign 1d
export GPG_PRIVATE_KEY="$(gpg --armor --export-secret-keys)"
gpg --armor --export > /tmp/test.asc
FPR=$(gpg --with-colons --list-keys | awk -F: '$1 == "fpr" { print $10; exit }')
unset GNUPGHOME

mkdir -p /tmp/repo
.github/scripts/publish-archlinux-repo.sh dist /tmp/repo richardcase
```

Then install from it into a throwaway root. pacman's default
`SigLevel = Required DatabaseOptional` applies, so the signatures are checked:

```bash
R=$(mktemp -d); mkdir -p "$R/root/var/lib/pacman" "$R/cache" "$R/gnupg"
cat > "$R/pacman.conf" <<EOF
[options]
Architecture = auto
SigLevel = Required DatabaseOptional
[richardcase]
Server = file:///tmp/repo/\$arch
EOF
GNUPGHOME="$R/gnupg" gpg --import /tmp/test.asc
echo "$FPR:6:" | GNUPGHOME="$R/gnupg" gpg --import-ownertrust
fakeroot pacman --config "$R/pacman.conf" --root "$R/root" \
  --dbpath "$R/root/var/lib/pacman" --cachedir "$R/cache" --gpgdir "$R/gnupg" \
  --disable-sandbox --noconfirm -Sy satchel
"$R/root/usr/bin/satchel" version
```

Or, with Docker, run a real install in a clean container:

```bash
docker run --rm -v /tmp/repo:/repo:ro -v /tmp/test.asc:/key.asc:ro archlinux:base bash -c '
  pacman-key --init && pacman-key --add /key.asc && pacman-key --lsign-key '"$FPR"' &&
  printf "\n[richardcase]\nServer = file:///repo/\$arch\n" >> /etc/pacman.conf &&
  pacman -Sy --noconfirm satchel && satchel version'
```
