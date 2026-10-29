# wsl2-serve-a-port

A colorful terminal app for exposing ports from WSL2 to the rest of your
network, so your phone, tablet, or a teammate's laptop can hit the dev server
running inside WSL.

> 🚧 Work in progress.

## Why

In WSL2's default NAT networking mode, Windows only forwards `localhost` into
the WSL VM. Other devices on your LAN can't reach services running in WSL. The
fix is a Windows `netsh interface portproxy` rule plus an inbound firewall
rule, both of which need admin rights and break whenever the WSL IP changes.
`serve-a-port` handles all of that for you.

## Requirements

- Windows 11 with WSL2
- Go 1.26+ (to build from source)

## Build

```sh
make build
./bin/serve-a-port
```

## Usage

Run `serve-a-port` with no arguments for the interactive dashboard: your WSL
and Windows LAN IPs, every forwarded port with a live status, and hints for
servers that are listening but not shared yet.

| Key | Action |
| --- | --- |
| `a` | Add ports: pick from servers running in WSL, or type `3000`, `8000-8010`, `3000, 5173` |
| `d` | Remove the selected forward |
| `D` | Remove every forward serve-a-port created |
| `r` | Repair forwards that point at an old WSL IP or lost their rules |
| `R` | Refresh |
| `?` | All keys |
| `q` | Quit (forwards stay in place) |

Every change shows exactly what will run, then goes through one Windows admin
(UAC) prompt.

The same operations are available as subcommands for scripting:

```sh
serve-a-port status            # environment checks, IPs, forwards, listeners
serve-a-port add 3000          # forward one port
serve-a-port add 8000-8010     # or a range (max 256 ports)
serve-a-port rm 3000           # stop forwarding
serve-a-port repair            # re-point forwards after the WSL IP changes
```

Changes go through a single Windows admin (UAC) prompt per command.
Forwards this app creates are tracked in
`~/.config/wsl2-serve-a-port/state.json`; existing portproxy rules made by
other tools are shown as `external` and never modified unless you remove
them explicitly.

## License

MIT
