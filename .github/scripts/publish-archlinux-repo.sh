#!/usr/bin/env bash
# Add freshly built .pkg.tar.zst packages to a signed pacman repository.
#
#   publish-archlinux-repo.sh <packages-dir> <repo-dir> <repo-name>
#
# GPG_PRIVATE_KEY must hold an ASCII-armoured secret key without a passphrase.
# It is imported into a throwaway GNUPGHOME, so running this on a workstation
# never touches the maintainer's own keyring.
#
# The resulting layout is <repo-dir>/<arch>/<repo-name>.db (+ .files, + .sig),
# which is what `Server = .../$arch` in pacman.conf expects. Needs repo-add,
# so it runs on Arch (the release workflow uses the archlinux:base image).
# See docs/releasing.md.
set -euo pipefail

if [[ $# -ne 3 ]]; then
	echo "usage: $0 <packages-dir> <repo-dir> <repo-name>" >&2
	exit 2
fi
pkgdir=$1
repodir=$2
name=$3

if [[ -z ${GPG_PRIVATE_KEY:-} ]]; then
	echo "GPG_PRIVATE_KEY is not set" >&2
	exit 1
fi

GNUPGHOME=$(mktemp -d)
export GNUPGHOME
trap 'rm -rf "$GNUPGHOME"' EXIT
printf '%s\n' "$GPG_PRIVATE_KEY" | gpg --batch --quiet --import
keyid=$(gpg --batch --with-colons --list-secret-keys | awk -F: '$1 == "fpr" { print $10; exit }')
if [[ -z $keyid ]]; then
	echo "no secret key found in GPG_PRIVATE_KEY" >&2
	exit 1
fi

shopt -s nullglob
pkgs=("$pkgdir"/*.pkg.tar.zst)
if [[ ${#pkgs[@]} -eq 0 ]]; then
	echo "no .pkg.tar.zst files in $pkgdir" >&2
	exit 1
fi

for pkg in "${pkgs[@]}"; do
	# goreleaser names files by GOARCH (amd64, arm64); pacman's $arch is
	# x86_64/aarch64. .PKGINFO is authoritative, so read it rather than
	# mapping one naming scheme onto the other.
	info=$(bsdtar -xOf "$pkg" .PKGINFO)
	field() { awk -F' = ' -v k="$1" '$1 == k { print $2; exit }' <<<"$info"; }
	pkgname=$(field pkgname)
	pkgver=$(field pkgver)
	arch=$(field arch)
	if [[ -z $pkgname || -z $pkgver || -z $arch ]]; then
		echo "$pkg: incomplete .PKGINFO" >&2
		exit 1
	fi

	mkdir -p "$repodir/$arch"
	# Use pacman's conventional file name, so a repo listing reads like any
	# other Arch mirror.
	dest="$repodir/$arch/$pkgname-$pkgver-$arch.pkg.tar.zst"
	cp "$pkg" "$dest"
	rm -f "$dest.sig"
	gpg --batch --yes --detach-sign --no-armor --local-user "$keyid" "$dest"

	# repo-add refuses to follow the plain-file copies made below, so drop
	# them and let it recreate the symlinks from the tarballs.
	for db in db files; do
		rm -f "$repodir/$arch/$name.$db" "$repodir/$arch/$name.$db.sig"
	done
	# --remove deletes the superseded package file and its signature, so the
	# repository holds only the current version of each package.
	repo-add --quiet --sign --key "$keyid" --remove \
		"$repodir/$arch/$name.db.tar.gz" "$dest"

	# GitHub Pages does not reliably serve symlinks, and pacman fetches
	# <name>.db, not <name>.db.tar.gz — so replace repo-add's links with copies.
	# repo-add's .old backups are for local rollback, not for publishing.
	for db in db files; do
		rm -f "$repodir/$arch/$name.$db.tar.gz.old" "$repodir/$arch/$name.$db.tar.gz.old.sig"
		for suffix in "" .sig; do
			link="$repodir/$arch/$name.$db$suffix"
			if [[ -L $link ]]; then
				cp --remove-destination "$(readlink -f "$link")" "$link"
			fi
		done
	done
	echo "published $pkgname $pkgver ($arch)"
done
