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
            echo "logx dev shell"
            echo "Go: $(go version)"
            echo "Common tasks: just test, just build, goreleaser check"
            # Drop into an interactive zsh, but only when running interactively
            # and zsh is available, so CI and non-zsh shells are not broken.
            if [ -z "$LOGX_NO_ZSH" ] && [ -t 1 ] && command -v zsh >/dev/null 2>&1; then
              exec zsh
            fi
          '';
        };
      }
    );
}
