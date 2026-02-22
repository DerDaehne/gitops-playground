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
            godef
          ];

          shellHook = ''
            export GOPATH="$HOME/go"
            export PATH="$GOPATH/bin:$PATH"
          '';
        };

        packages.${system}.default = pkgs.buildGoModule {
          pname = "gitops-playground";
          version = "0.1.0";
          src = ./.;
          vendorHash = "sha256-VTw/W99Sr2vsK6H234WfgKwmjBd4Gv6oTNtWOu8xfTM=";
        };

      };
}
