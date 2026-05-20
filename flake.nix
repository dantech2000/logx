{
  description = "logx development environment";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs =
    { nixpkgs, flake-utils, ... }:
    flake-utils.lib.eachDefaultSystem (
      system:
      let
        pkgs = import nixpkgs { inherit system; };
        go = pkgs.go_1_26;
      in
      {
        devShells.default = pkgs.mkShell {
          packages = with pkgs; [
            go
            goreleaser
            golangci-lint
            gotools
            just
            kubectl
          ];

          env = {
            CGO_ENABLED = "0";
          };

          shellHook = ''
            exec zsh
            echo "logx dev shell"
            echo "Go: $(go version)"
            echo "Common tasks: just test, just build, goreleaser check"
          '';
        };
      }
    );
}
