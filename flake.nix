{
  description = "gitops-playground - Creates a complete GitOps-based operational stack on your Kubernetes clusters";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
  };

  outputs = { self, nixpkgs }:
    let
      system = "x86_64-linux";
      pkgs = nixpkgs.legacyPackages.${system};
    in
      {

        devShells.${system}.default = pkgs.mkShell {
          buildInputs = with pkgs; [
            go
            gopls
            golangci-lint
          ];

          shellHook = ''
            export GOPATH="$HOME/go"
            export PATH="$GOPATH/bin:$PATH"
          '';
        };

        packages.default = pkgs.buildGoModule {
          pname = "gop";
          version = "0.1.0";
          src = ./.;
          vendorHash = null;
        };

      };
}
