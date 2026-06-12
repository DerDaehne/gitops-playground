{
  description = "gitops-playground Go port — dev shell, build and container for cmd/gop";

  inputs = {
    # nixos-unstable currently ships Go 1.25+, which easily satisfies the
    # `go 1.22` directive in go.mod.
    nixpkgs.url     = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = import nixpkgs { inherit system; };

        # Runtime tools the Go binary shells out to. They are added to the
        # dev shell AND wrapped onto the built binary's PATH so installed
        # builds find helm/kubectl deterministically – not via the user's
        # ambient PATH.
        runtimeTools = [
          pkgs.kubectl
          pkgs.kubernetes-helm
          pkgs.git
        ];

        # The Go module sits at the repo root after the Groovy original
        # was retired into ./retired (Phase 8 of the port).
        gop = pkgs.buildGoModule {
          pname   = "gop";
          version = "0.1.0-dev";
          src     = ./.;

          subPackages = [ "cmd/gop" ];

          # Pinned via `nix build` against go.sum. To refresh after a
          # go.mod / go.sum change: replace with `pkgs.lib.fakeHash`,
          # run `nix build`, copy the printed sha256 back here.
          vendorHash = "sha256-Fo708koTJeD1sXxqYYeDq/K6cGDtCOfEMaSZp3zUP6g=";

          env.CGO_ENABLED = 0;

          ldflags = [
            "-s" "-w"
            "-X" "github.com/cloudogu/gitops-playground/go/internal/cli.Version=${self.shortRev or "dev"}"
            "-X" "github.com/cloudogu/gitops-playground/go/internal/cli.Commit=${self.rev or "unknown"}"
          ];

          # Run `go test ./...` during the build so `nix build` /
          # `nix flake check` exercise the suite.
          doCheck = true;

          nativeBuildInputs = [ pkgs.makeWrapper ];
          postInstall = ''
            wrapProgram $out/bin/gop \
              --prefix PATH : ${pkgs.lib.makeBinPath runtimeTools}
          '';

          meta = with pkgs.lib; {
            description = "gitops-playground CLI (Go port)";
            homepage    = "https://github.com/cloudogu/gitops-playground";
            license     = licenses.agpl3Only;
            mainProgram = "gop";
          };
        };

        # OCI image. `nix build .#oci` produces a load-able tarball; push
        # with skopeo or `docker load < $(nix build .#oci --print-out-paths)`.
        oci = pkgs.dockerTools.buildLayeredImage {
          name = "gop";
          tag  = "latest";

          contents = [
            gop
            pkgs.kubernetes-helm
            pkgs.kubectl
            pkgs.cacert
          ];

          config = {
            Entrypoint = [ "${gop}/bin/gop" ];
            WorkingDir = "/workspace";
            User       = "65532:65532";
            Env = [
              "SSL_CERT_FILE=${pkgs.cacert}/etc/ssl/certs/ca-bundle.crt"
              "PATH=/bin"
            ];
            Labels = {
              "org.opencontainers.image.source"      = "https://github.com/cloudogu/gitops-playground";
              "org.opencontainers.image.title"       = "gop";
              "org.opencontainers.image.description" = "GitOps Playground CLI (Go port)";
            };
          };
        };
      in {
        packages = {
          default = gop;
          gop     = gop;
          oci     = oci;
        };

        # mkApp from flake-utils does not accept a meta attribute; the
        # meta block is appended after the fact. nix flake check
        # otherwise warns "app lacks attribute 'meta'".
        apps.default = (flake-utils.lib.mkApp { drv = gop; }) // {
          meta = gop.meta;
        };

        devShells.default = pkgs.mkShell {
          packages = [
            pkgs.go
            pkgs.gopls
            pkgs.gotools
            pkgs.go-tools
            pkgs.golangci-lint
            pkgs.delve
            pkgs.gnumake
            pkgs.git
            pkgs.kubernetes-helm
            pkgs.kubectl
            # container build helpers
            pkgs.docker-buildx
            pkgs.skopeo
          ];

          shellHook = ''
            echo "gitops-playground devshell"
            echo "  go      : $(go version 2>/dev/null || echo n/a)"
            echo "  helm    : $(helm version --short 2>/dev/null || echo n/a)"
            echo "  kubectl : $(kubectl version --client=true 2>/dev/null | head -n1 || echo n/a)"
            export GOFLAGS="-mod=mod"
          '';
        };

        # `nix flake check` builds gop (which runs `go test ./...` via
        # buildGoModule's doCheck). golangci-lint cannot run inside a Nix
        # sandbox because it needs the module graph that Nix only fetches
        # for the build derivation – we run lint as a separate CI job
        # with networked Go instead.
        checks = {
          build = gop;
        };

        formatter = pkgs.nixpkgs-fmt;
      });
}
