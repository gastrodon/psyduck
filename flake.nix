{
  description = "psyduck: an ETL pipeline runner";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs =
    { self, nixpkgs, flake-utils }:
    flake-utils.lib.eachDefaultSystem (
      system:
      let
        pkgs = nixpkgs.legacyPackages.${system};
      in
      {
        packages.default = pkgs.buildGoModule {
          pname = "psyduck";
          version = self.shortRev or self.dirtyShortRev or "dev";

          src = self;
          vendorHash = "sha256-pUYDsfJMTZEfLcY001Dlc9djCfy+keH60SXmEkiWOe8=";

          # Only the root command builds a runnable binary; the rest of the
          # module is libraries (and stdlib/integration, which is test-only
          # and has no buildable package of its own).
          subPackages = [ "." ];

          # plugins/store.go loads compiled plugins through the standard
          # library's `plugin` package, which needs cgo -- matches the
          # project's own Dockerfile, which builds the same way.
          env.CGO_ENABLED = 1;

          ldflags = [
            "-s" # omit the symbol table
            "-w" # omit DWARF debug info
          ];
          # nixpkgs adds -trimpath to GOFLAGS by default, keeping
          # builder-specific paths (e.g. /build) out of the binary.

          # plugins/fetch_test.go shells out to `git` during the build's
          # checkPhase (go test ./...).
          nativeBuildInputs = [ pkgs.git ];

          # psyduck itself shells out to `go` and `git` at runtime to fetch
          # and build plugins (plugins/fetch.go), same as a plain `go
          # build`-produced binary would: it relies on the caller's PATH,
          # so we don't bundle or wrap either tool in here.

          meta = {
            description = "An ETL pipeline runner";
            homepage = "https://github.com/gastrodon/psyduck";
            mainProgram = "psyduck";
          };
        };

        apps.default = flake-utils.lib.mkApp {
          drv = self.packages.${system}.default;
        };
      }
    );
}
