let
  nixpkgs = fetchTarball "https://github.com/NixOS/nixpkgs/tarball/nixos-23.11";
  pkgs = import nixpkgs { config = {}; overlays = []; };
in

pkgs.mkShell {
  buildInputs = with pkgs; [
    docker
    go
    git
    gopls
    lunarvim
    zellij
  ];

  shellHook = ''
    echo "Welcome to your nix-shell environment!"
    
    # Setting aliases
    alias vim=lvim
    
    # Launch zellij using zsh if it is installed
    if type zsh > /dev/null 2>&1; then
        zellij --layout layout.kdl options --simplified-ui true --default-shell zsh
    else
        zellij --layout layout.kdl options --simplified-ui true
    fi
  '';
}
