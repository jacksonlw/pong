# Pong

Pong collects every question, plan and permission request from your coding agents and shows each one as a Ping. You answer with a Pong, and the agent carries on.

This repository is the part of Pong that runs on your own computers: the Engine, the `pong` command, the Mac app, and what `pong setup` adds to Claude Code, Codex and other agents. It works with no account and no network beyond your own. Computers on the same network can share their Pings with each other.

## Install

On a Mac with Homebrew:

```bash
brew tap jacksonlw/pong https://github.com/jacksonlw/pong
```

```bash
brew install --cask jacksonlw/pong/pong
```

Without Homebrew, or on Linux, download a release from https://github.com/jacksonlw/pong/releases and run its installer as a separate step:

```bash
bash install.sh Pong-VERSION.zip
```

Then set Pong up in every agent it finds, and answer the test Ping it sends:

```bash
pong setup
```

## Build from source

You need Go and, for the Mac app, Xcode.

```bash
go build -o bin/ ./cmd/...
```

```bash
go test ./...
```

```bash
scripts/build-app.sh
```

`scripts/build-app.sh` writes an unsigned `build/Pong.app` with `pong-engine` and `pong` inside it. To try the Engine without touching your real data, give it a scratch folder:

```bash
PONG_HOME=$(mktemp -d) go run ./cmd/pong-engine -port 0
```

## Licence

Pong is source-available under the Functional Source License 1.1 with Apache 2.0 as its future licence (`LICENSE.md`). You may read, run and change it for any purpose except offering a product that competes with Pong. Each release becomes Apache 2.0 two years after it ships.
