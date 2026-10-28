# noebot: thin private-application consumer

This example contains **no application implementation or bundle**. It imports
the public `main(configPath, {env})` of a separately extracted private noebot
installation. There is no sibling-checkout fallback, checkout link or npm/Node
dependency.

## Prerequisites and installation

Linux, NOE >= 2.10.0 on PATH (including the child host), util-linux flock and
GNU coreutils sync with -f are required. Provider credentials/configuration
belong to NOE, outside noebot's sample JSON. Telegram is optional; its token
and approved user/chat IDs are configured as described in the installed
`docs/operations.md`. Python/Firefox are developer tests only.

Obtain the private noebot tar and checksum from the owner's chosen delivery
route. No registry, remote repository or invented license is needed. Check
against a trusted checksum, then extract; for VERSION 0.1.0:

```sh
# In the directory containing the delivered tar and checksum:
sha256sum -c noebot-0.1.0.tar.gz.sha256
mkdir -p "$HOME/opt"
tar -xzf noebot-0.1.0.tar.gz -C "$HOME/opt"
export NOEBOT_HOME="$HOME/opt/noebot-0.1.0"
mkdir -p "$HOME/workspace" "${XDG_CONFIG_HOME:-$HOME/.config}/noebot"
chmod 700 "${XDG_CONFIG_HOME:-$HOME/.config}/noebot"
# From the NOE repository root (or copy this example directory elsewhere):
cp examples/noebot/config.example.json "${XDG_CONFIG_HOME:-$HOME/.config}/noebot/config.json"
chmod 600 "${XDG_CONFIG_HOME:-$HOME/.config}/noebot/config.json"
# Edit config.json: real absolute existing workspace, chosen IANA timezone,
# provider/model/effort. Configure NOE provider credentials separately.
noe run --no-inst examples/noebot/main.js
# Optional explicit config path (relative paths resolve from the invoking cwd):
# noe run --no-inst examples/noebot/main.js /absolute/path/config.json
```

Keep any provider/Telegram environment files outside the installation and
workspace in a protected directory (0700), with files readable/writable only
by the chosen account (0600). Never commit or publish them.

`NOEBOT_HOME` must be an **absolute extracted installation root**, containing
`app/main.mjs`, not the workspace or app subdirectory. The loader resolves it
with realpath and imports a file URL. Missing/bad/relative env values produce
clear path-free errors, never silently load a checkout. The entry accepts only
zero or one config path and propagates the returned exit code with process.exit;
startup errors exit 1, extra arguments exit 2. There is **no app --help flag**.
Omitting the path uses XDG_CONFIG_HOME/noebot/config.json, with HOME/.config
fallback for unset/empty XDG_CONFIG_HOME.

The application bootstrap starts its locked child host in the configured
workspace as the **actual cwd**, with explicit common/app --inst directories;
workspace instructions are not automatically selected. Notes are ordinary
context, not system instructions. Resources load relative to the installation,
not the cwd. Open the exact printed `http://127.0.0.1:PORT` origin (not localhost).

The installation's README and docs/operations.md are authoritative for Web
controls, Telegram commands, same-origin curl/API requirements, storage/backup,
limits, shutdown, notifications and candidate uninstalled systemd/SSH examples.
No live provider/Telegram/vm200 deployment is verified here. NOE is not a
sandbox: account permissions and sensitive NOE logs remain the operator boundary.
Restart means fresh conversations, persisted notes/schedules/results and active
jobs interrupted, with no automatic resume/replay or outgoing delivery retries.
Web sent means local enqueue only; Telegram sent means platform acceptance,
neither means read.
