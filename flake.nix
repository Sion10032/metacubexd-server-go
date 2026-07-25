{
  description = "metacubexd-server-go";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = import nixpkgs { inherit system; };
        version = "0.1.0";
      in
      {
        packages = {
          metacubexd-server-go = pkgs.buildGoModule {
            pname = "metacubexd-server-go";
            inherit version;
            src = ./.;
            vendorHash = "sha256-m+E/IP5Iv5GcQIR4OEBEyUtWgOtijw2HtxBhhMNwwls=";
            subPackages = [ "cmd/metacubexd-server" ];
            buildFlags = [ "-trimpath" ];
            ldflags = [
              "-s" "-w"
              "-X main.version=${version}"
            ];
            postInstall = ''
              mv $out/bin/metacubexd-server $out/bin/metacubexd-server-go
            '';
            meta = with pkgs.lib; {
              description = "ClashMeta Dashboard backend server";
              homepage = "https://github.com/Sion10032/metacubexd-server-go";
              license = licenses.mit;
              platforms = platforms.linux ++ platforms.darwin;
              mainProgram = "metacubexd-server-go";
            };
          };

          default = self.packages.${system}.metacubexd-server-go;
        };

        devShells.default = pkgs.mkShellNoCC {
          packages = with pkgs; [
            go
            gopls
            gotools
          ];
        };
      }
    );
}
