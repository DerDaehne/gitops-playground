{
  description = "gitops-playground — installs a complete GitOps platform on Kubernetes";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
  };

  outputs = { self, nixpkgs }:
    let
      supportedSystems = [ "x86_64-linux" "aarch64-linux" "aarch64-darwin" "x86_64-darwin" ];
      forAllSystems = nixpkgs.lib.genAttrs supportedSystems;
    in {
      devShells = forAllSystems (system:
        let pkgs = nixpkgs.legacyPackages.${system}; in {
          default = pkgs.mkShell {
            buildInputs = with pkgs; [
              go
              gopls
              golangci-lint
              godef
              kubectl
              helm
              k9s
            ];
            shellHook = ''
              export GOPATH="$HOME/go"
              export PATH="$GOPATH/bin:$PATH"
            '';
          };
        });

      packages = forAllSystems (system:
        let pkgs = nixpkgs.legacyPackages.${system}; in {
          default = pkgs.buildGoModule {
            pname = "gitops-playground";
            version = "0.1.0";
            src = ./.;
            vendorHash = null; # update after go mod vendor
          };
        });
    };
}
