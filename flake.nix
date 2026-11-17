{
  description = "Install, update and remove agent skills from git";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs =
    { self, nixpkgs }:
    let
      systems = [
        "x86_64-linux"
        "aarch64-linux"
        "aarch64-darwin"
      ];
      forAllSystems = f: nixpkgs.lib.genAttrs systems (system: f nixpkgs.legacyPackages.${system});

      # A flake cannot see the tag it was fetched at, so the version comes from
      # the commit instead; see docs/adr/0001-nix-flake-version-from-git.md.
      ts = self.lastModifiedDate;
      day = "${builtins.substring 0 4 ts}-${builtins.substring 4 2 ts}-${builtins.substring 6 2 ts}";
      version = "0-unstable-${day}";
      rev = self.shortRev or self.dirtyShortRev or "unknown";
      date = "${day}T${builtins.substring 8 2 ts}:${builtins.substring 10 2 ts}:${builtins.substring 12 2 ts}Z";
    in
    {
      packages = forAllSystems (pkgs: {
        default = pkgs.buildGoModule {
          pname = "satchel";
          inherit version;
          src = self;
          vendorHash = "sha256-u25VTQojMkzMHHmS7h5uidhkONrgKc+iUIYKnCqNrP8=";

          env.CGO_ENABLED = "0";
          ldflags = [
            "-s"
            "-w"
            "-X github.com/richardcase/satchel/internal/buildinfo.version=${version}"
            "-X github.com/richardcase/satchel/internal/buildinfo.commit=${rev}"
            "-X github.com/richardcase/satchel/internal/buildinfo.date=${date}"
          ];

          nativeBuildInputs = [ pkgs.makeWrapper ];
          nativeCheckInputs = [ pkgs.git ];
          __darwinAllowLocalNetworking = true;

          postInstall = ''
            rm "$out/bin/registry-check"
            wrapProgram "$out/bin/satchel" --suffix PATH : ${pkgs.lib.makeBinPath [ pkgs.git ]}
          '';

          meta = {
            description = "Install, update and remove agent skills from git";
            homepage = "https://github.com/richardcase/satchel";
            license = pkgs.lib.licenses.asl20;
            mainProgram = "satchel";
            platforms = systems;
          };
        };
      });

      apps = forAllSystems (pkgs: {
        default = {
          type = "app";
          program = "${self.packages.${pkgs.stdenv.hostPlatform.system}.default}/bin/satchel";
          meta.description = "Install, update and remove agent skills from git";
        };
      });

      devShells = forAllSystems (pkgs: {
        default = pkgs.mkShell {
          packages = [
            pkgs.go
            pkgs.golangci-lint
            pkgs.goreleaser
          ];
          GOTOOLCHAIN = "local";
        };
      });

      formatter = forAllSystems (pkgs: pkgs.nixfmt-tree);
    };
}
