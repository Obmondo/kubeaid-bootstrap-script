{
  description = "KubeAid Bootstrap Script development environment";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs =
    {
      self,
      nixpkgs,
      flake-utils,
    }:
    flake-utils.lib.eachDefaultSystem (
      system:
      let
        pkgs = import nixpkgs {
          inherit system;
          config.allowUnfree = true;
        };
      in
      with pkgs;
      {
        devShells.default = mkShell {
          nativeBuildInputs = [
            go
            golangci-lint
          ];

          buildInputs = [
            # Required for building KubePrometheus.
            gojsontoyaml
            jsonnet
            jsonnet-bundler
            jq

            kubectl
            kubeone

            k3d
            clusterctl
          ];
        };
      }
    );
}
