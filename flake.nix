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
          vendorHash = "sha256-qVw7knLN99RNQ0pbTVP7ibTzH1kboJ9vwZZqMM4IEMw=";

          # Only the root command builds a runnable binary; the rest of the
          # module is libraries (and stdlib/integration, which is test-only
          # and has no buildable package of its own).
          subPackages = [ "." ];

          # Force a pure-Go build. Nothing in psyduck imports C, but the
          # default (CGO_ENABLED=1, since the Nix build stdenv has a C
          # compiler on PATH) still swaps in cgo-backed `net` and
          # `os/user`, which dynamically link against glibc from the build
          # closure. Disabling cgo gives a fully static, portable binary
          # and drops glibc from the runtime closure.
          env.CGO_ENABLED = 0;

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
