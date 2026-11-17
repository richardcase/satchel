# Nix flake builds take their version from git, not the release tag

A flake cannot see which tag it was fetched at: `self` exposes the commit's
`rev`, `shortRev` and `lastModifiedDate`, but no ref. So a Nix build reports
`0-unstable-YYYY-MM-DD` (the nixpkgs convention for unversioned builds), with
the short rev as the commit and the commit time as the date, instead of the
semver that goreleaser stamps. We chose this over a version hardcoded in
`flake.nix` because that needs a bump before every release and a release-time
job to check it, and it reports a release version for `main` builds that are
not that release. A Nix build of `v0.14.0` therefore does not say `0.14.0`;
the rev identifies it.

## Considered Options

- **Hardcode the version in `flake.nix`, and check it in `release.yml`.**
  Rejected: it adds a manual step and a job to every release, and a missed
  bump blocks the release.
- **Have the release job set the version.** Not possible: the tag pins the
  tree before the job runs, and the `main` ruleset (pull request, signed
  commits) stops the job from pushing a fix.
