# Background Service

Nectar can install itself as a background service for the current user:

- macOS: LaunchAgent; starts after login and restarts after a crash
- Linux: systemd user service; can start at boot with linger

The service starts `nectar -config ~/.nectar/config.toml` using the absolute paths from installation time.

> Background service support requires v0.1.1 or later.

## Configure first, then install the service

Service mode reads the selected platform's (Feishu or DingTalk) credentials from `~/.nectar/config.toml` and does not inherit `FEISHU_APP_ID` / `FEISHU_APP_SECRET` or `DINGTALK_CLIENT_ID` / `DINGTALK_CLIENT_SECRET` from the shell.

Edit the file first:

```bash
~/.nectar/config.toml
```

Confirm that the selected platform's configuration contains real values:

- Feishu: `app_id` / `app_secret` under `[feishu]`
- DingTalk: `client_id` / `client_secret` under `[dingtalk]` (legacy `app_key` / `app_secret` are also supported)

If both Feishu and DingTalk are configured, set `platform = "feishu"` or `platform = "dingtalk"` explicitly under `[bot]`. Then run:

```bash
nectar service install
```

The one-line installer writes the configuration and installs and starts the background service by default. To install the binary only:

```bash
curl -fsSL https://raw.githubusercontent.com/zhouyg1992/nectar/main/install.sh | sh -s -- --no-service
```

On Linux, add `--linger`:

```bash
curl -fsSL https://raw.githubusercontent.com/zhouyg1992/nectar/main/install.sh | sh -s -- --linger
```

If the configuration still contains example placeholders, service installation fails immediately and asks you to edit the configuration first.

## Common commands

```bash
nectar service status
nectar service logs
nectar service logs -n 500
nectar service logs -f
nectar service restart
nectar service stop
nectar service start
nectar service uninstall
```

`nectar service install --no-start` writes and enables the service without starting it immediately.

## macOS

Service file:

```text
~/Library/LaunchAgents/com.zhouyg1992.nectar.plist
```

Logs:

```text
~/Library/Logs/nectar.out.log
~/Library/Logs/nectar.err.log
```

The LaunchAgent starts **after the user logs in**. If nobody logs in after a Mac restart, Nectar does not run. For true 24/7 operation, use an always-on Linux server, NAS, or Mac mini.

Uninstalling only removes the service file; configuration, data, and logs are kept:

```bash
nectar service uninstall
```

## Linux

Service file:

```text
~/.config/systemd/user/nectar.service
```

Logs:

```bash
journalctl --user -u nectar.service -f
```

By default, a systemd user service starts after the user logs in. To start at boot without login:

```bash
nectar service install --linger
```

If permissions are insufficient, run manually:

```bash
sudo loginctl enable-linger "$USER"
```

## Paths and environment

The service environment does not load `.zshrc` / `.bashrc`. The following are written into the service definition:

- `HOME`
- `PATH` (including Nectar's directory and common system paths)
- `CODEX_HOME` (if set in the current shell)

If `codex` / `opencode` / `cursor-agent` is installed through nvm, asdf, or another version manager, make sure its directory is in `PATH` when installing the service, or point the corresponding engine's `path` in `~/.nectar/config.toml` at the executable.

## Troubleshooting

| Symptom | Check |
|---|---|
| Service installation fails with a placeholder configuration error | Edit `~/.nectar/config.toml` first |
| The service exits immediately after starting | Run `nectar service logs` to inspect engine/platform errors (Feishu or DingTalk) |
| Engine CLI not found | Check the service `PATH`, or set `codex.path` / `opencode.path` / `cursor.path` in the configuration |
| Cursor reports not logged in | Run `cursor-agent login` as the same user running the service, or set `CURSOR_API_KEY` |
| Two instances compete for Feishu/DingTalk events | Stop the manually started `nectar` and keep only the service instance |
| Messages are lost after macOS sleep | The long connection disconnects during sleep and reconnects after wake; messages sent while asleep are not replayed |
| Linux does not start at boot | Check whether `loginctl show-user "$USER" -p Linger` is `yes` |

## Security

- The service file does not contain `app_secret` / `client_secret`; credentials remain in `~/.nectar/config.toml` with `0600` permissions.
- Do not run personal Nectar as root; engine credentials and workspaces live in the user's home directory.
- Logs may contain chat IDs, model information, and error context; pay attention to file permissions and rotation.
