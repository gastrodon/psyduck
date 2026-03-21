{
  description = "psyduck — the psychic ETL";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = nixpkgs.legacyPackages.${system};
      in {
        packages.default = pkgs.buildGoModule {
          pname = "psyduck";
          version = "0.1.0";
          src = ./.;
          vendorHash = "sha256-yHVBxf2VVx8ehSPQdu9jALVi4NHjrDx99OZ+S/I7XeI=";
          proxyVendor = true;
        };

        devShells.default = pkgs.mkShell {
          buildInputs = with pkgs; [
            go
            gopls
            gotools
          ];

          shellHook = ''
            mkdir -p .git/hooks
            cat > .git/hooks/pre-commit << 'EOF'
            #!/usr/bin/env sh
            set -e

            go build -o /dev/null .

            if git diff --cached --name-only | grep -q '^core/'; then
              go test ./...
            else
              go test -short ./...
            fi

            STAGED=$(git diff --cached --name-only --diff-filter=ACM | grep '\.go$' || true)
            if [ -n "$STAGED" ]; then
              gofmt -w $STAGED
              git add $STAGED
            fi
            EOF
            chmod +x .git/hooks/pre-commit
          '';
        };
      });
}
