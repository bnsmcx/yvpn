# Contributing to yVPN

Thanks for your interest in contributing to yVPN!

## Getting Started

1. Fork the repository
2. Clone your fork: `git clone https://github.com/YOUR_USERNAME/yvpn.git`
3. Create a branch: `git checkout -b my-feature`
4. Make your changes
5. Test your changes
6. Commit: `git commit -m "Add my feature"`
7. Push: `git push origin my-feature`
8. Open a Pull Request

## Development Setup

### Prerequisites

- Go 1.21+
- DigitalOcean API token (for testing)
- Tailscale API key (for testing)

### Using Nix

```bash
nix-shell
```

### Manual Setup

```bash
go mod download
go build -o yvpn ./cmd/tui
```

## Code Style

- Run `go fmt` before committing
- Follow standard Go conventions
- Keep functions focused and small

## Reporting Issues

When reporting bugs, please include:

- yVPN version (`yvpn --version`)
- Operating system and architecture
- Steps to reproduce the issue
- Expected vs actual behavior

## Pull Requests

- Keep PRs focused on a single change
- Update documentation if needed
- Add tests for new functionality when possible

## Questions?

Open an issue for any questions about contributing.
