# serve-a-port

A colorful terminal app that shares ports from WSL2 with every device on your
network, so your phone, tablet, or a teammate's laptop can open the dev server
running inside WSL.

```
  ╔═╗╔═╗╦═╗╦  ╦╔═╗  ╔═╗  ╔═╗╔═╗╦═╗╔╦╗
  ╚═╗║╣ ╠╦╝╚╗╔╝║╣ ──╠═╣──╠═╝║ ║╠╦╝ ║
  ╚═╝╚═╝╩╚═ ╚╝ ╚═╝  ╩ ╩  ╩  ╚═╝╩╚═ ╩
  beam your WSL2 ports to every device on the network ✨

  ╭───────────────────────╮  ╭────────────────────────────────╮
  │  🐧 WSL2              │  │  ⊞ Windows LAN                 │
  │  172.28.112.5         │  │  192.168.1.42                  │
  │  eth0 · Ubuntu-24.04  │  │  Wi-Fi                         │
  ╰───────────────────────╯  │  + 100.101.102.103  Tailscale  │
                             ╰────────────────────────────────╯
  NAT mode · 5 forwards · updated 10:39:57

  ╭────────────────────────────────────────────────────────╮
  │   PORT  STATUS       PROCESS  SHARE URL                │
  ├────────────────────────────────────────────────────────┤
  │ ▸ 3000  ● live       node     http://192.168.1.42:3000 │
  │   4321  ↻ stale      —        http://192.168.1.42:4321 │
  │   5173  ● live       vite     http://192.168.1.42:5173 │
  │   8000  ○ idle       —        http://192.168.1.42:8000 │
  │   8080  ◐ localhost  python3  http://192.168.1.42:8080 │
  ╰────────────────────────────────────────────────────────╯
  ✨ Live! Anyone on your network can open http://192.168.1.42:3000
  💡 storybook is listening on :6006. Press a to share it.

  a add port • d remove • r repair • enter qr code • c copy url • ? more • q quit
```

<sub>Captured from `serve-a-port demo`, which uses made-up data. In a real
terminal it's in full synthwave color.</sub>

## Features

- **Add ports your way:** pick from the servers already listening in WSL, or
  type a port, a range (`8000-8010`), or a list (`3000, 5173`).
- **One admin prompt per change:** every change shows exactly what will run,
  then goes through a single Windows UAC prompt.
- **Live status:** start a dev server and its row flips to `● live` within a
  few seconds; you get a toast when a forward goes live or stops.
- **Survives WSL restarts:** when WSL comes back with a new IP, affected
  forwards turn `↻ stale` and one key re-points them.
- **QR codes:** press `enter` on a forward and scan it with your phone. `←/→`
  switches between networks (Wi-Fi, Ethernet, VPNs like Tailscale or
  ZeroTier).
- **Plays nicely with others:** rules created by other tools show as
  `◆ external` and are never touched unless you explicitly remove or replace
  them.
- **Catches the classic gotchas:** servers bound only to `127.0.0.1`, a
  stopped IP Helper service, Group Policy blocking firewall rules.
- **Scriptable:** every action is also a plain subcommand.

## Requirements

- Windows 11 with WSL2 (default NAT networking or mirrored mode)
- Windows interop enabled in WSL (the default)
- Go 1.26+ to build from source

## Install

```sh
go install github.com/jamaalsawyerd/wsl2-serve-a-port/cmd/serve-a-port@latest
```

Or from a clone:

```sh
make build && ./bin/serve-a-port
```

Want to look around first? `serve-a-port demo` runs the whole UI on made-up
data and changes nothing.

## Usage

Run `serve-a-port` for the dashboard.

| Key | Action |
| --- | --- |
| `a` | Add ports: pick running servers, or type `3000`, `8000-8010`, `3000, 5173` |
| `d` | Remove the selected forward |
| `D` | Remove every forward serve-a-port created |
| `r` | Repair forwards that point at an old WSL IP or lost their rules |
| `enter` | Show a QR code for the selected port (`←/→` switches network) |
| `c` | Copy the share URL to the Windows clipboard |
| `R` | Refresh |
| `?` | All keys |
| `q` | Quit, choosing whether to keep or tear down your forwards |

### Statuses

| Status | Meaning |
| --- | --- |
| `● live` | Forwarded, and a server in WSL is accepting connections |
| `○ idle` | Forwarded, but nothing is listening on that port yet |
| `◐ localhost` | The server only listens on `127.0.0.1`, so the forward can't reach it |
| `↻ stale` | The rule points at an old WSL IP; press `r` |
| `✗ missing` | serve-a-port created it, but the Windows rules are gone; press `r` |
| `◆ external` | A portproxy rule created by something else |

### Subcommands

```sh
serve-a-port status            # environment checks, IPs, forwards, listeners
serve-a-port add 3000          # forward one port
serve-a-port add 8000-8010     # or a range (max 256 ports)
serve-a-port rm 3000           # stop forwarding
serve-a-port repair            # re-point forwards after the WSL IP changes
serve-a-port demo              # the dashboard on made-up data
serve-a-port version
```

## How it works

In WSL2's default **NAT** mode, Windows forwards only `localhost` into the WSL
VM, so other devices can't reach WSL servers. For each port, serve-a-port
creates, as administrator:

1. a `netsh interface portproxy` rule from `0.0.0.0:<port>` on Windows to
   `<WSL IP>:<port>`, and
2. an inbound Windows Firewall rule in the `WSL2 Serve-a-Port` group.

All changes in one action run as a single PowerShell script through one UAC
prompt. The script also starts the IP Helper service (`iphlpsvc`) if it's
stopped, because portproxy passes no traffic without it.

In **mirrored** networking mode, WSL already shares Windows' IP addresses,
so serve-a-port creates only Hyper-V firewall rules.

Forwards created by serve-a-port are recorded in
`~/.config/wsl2-serve-a-port/state.json`. Rules and firewall entries persist
across reboots.

## Troubleshooting

**`◐ localhost`: the server only listens on 127.0.0.1.** Bind it to all
interfaces instead:

| Tool | Command |
| --- | --- |
| Vite | `vite --host` |
| Next.js | `next dev -H 0.0.0.0` |
| Create React App | `HOST=0.0.0.0 npm start` |
| Django | `python manage.py runserver 0.0.0.0:8000` |
| Flask | `flask run --host 0.0.0.0` |
| Rails | `rails server -b 0.0.0.0` |
| Python | `python3 -m http.server --bind 0.0.0.0` |

**It's forwarded and live, but other devices still can't connect.** Make sure
both devices are on the same network, and that the network isn't a guest or
"client isolation" Wi-Fi. Corporate machines may have firewall rules locked
down by Group Policy; serve-a-port reports this when Windows refuses a
change.

**After a reboot, nothing works.** WSL probably got a new IP. Open
serve-a-port and press `r`, or run `serve-a-port repair`.

**Removing everything by hand.** In an admin PowerShell:

```powershell
netsh interface portproxy reset
Remove-NetFirewallRule -Group 'WSL2 Serve-a-Port'
```

Note that `portproxy reset` also removes rules created by other tools.

## Development

```sh
make build      # bin/serve-a-port, stamped with git describe
make test       # unit, interaction, and end-to-end tests
make lint       # golangci-lint
make demo       # run the UI on made-up data
```

Some tests talk to real PowerShell (to syntax-check the generated scripts) and
skip themselves outside WSL.

To record a demo GIF with [VHS](https://github.com/charmbracelet/vhs):

```sh
vhs demo.tape   # writes docs/demo.gif
```

Built with [Bubble Tea](https://github.com/charmbracelet/bubbletea),
[Bubbles](https://github.com/charmbracelet/bubbles),
[Lip Gloss](https://github.com/charmbracelet/lipgloss),
[Huh](https://github.com/charmbracelet/huh), and
[Harmonica](https://github.com/charmbracelet/harmonica).

## License

MIT
