{
  description = "P2P terminal sharing with E2E encryption";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = nixpkgs.legacyPackages.${system};
        version = "1.4.1";
      in
      {
        packages = {
          default = self.packages.${system}.terminal-tunnel;

          terminal-tunnel = pkgs.buildGoModule {
            pname = "terminal-tunnel";
            inherit version;

            src = ./.;

            # Run `nix build` and it will tell you the correct hash
            vendorHash = "sha256-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=";

            ldflags = [
              "-s" "-w"
              "-X main.version=${version}"
            ];

            subPackages = [ "cmd/terminal-tunnel" ];

            postInstall = ''
              mv $out/bin/terminal-tunnel $out/bin/tt
            '';

            meta = with pkgs.lib; {
              description = "P2P terminal sharing with E2E encryption";
              homepage = "https://github.com/artpar/terminal-tunnel";
              license = licenses.mit;
              maintainers = [];
              mainProgram = "tt";
            };
          };
        };

        apps.default = {
          type = "app";
          program = "${self.packages.${system}.terminal-tunnel}/bin/tt";
        };

        devShells.default = pkgs.mkShell {
          buildInputs = with pkgs; [
            go_1_22
            gopls
            gotools
          ];
        };
      }
    );
}
