# Codex fixtures: written from documentation, NOT recorded

Every `*.json` here was **written by hand** on 2026-11-18 from Codex's hooks documentation,
https://learn.chatgpt.com/docs/hooks (reached from developers.openai.com/codex/hooks), read that
day. None was recorded from a real Codex: codex-cli 0.170.0 was installed on the Mac but not signed
in, so no turn could run and no hook could fire. **Behaviour of the hook against the real Codex is
unverified.** Field values (`thr_doc1`, `turn_1`, `/tmp/proj`, the model name `gpt-5.5-codex`) are
placeholders; the documentation gives no full example payload for these events.

| File | Doc section used | What it stands for |
|---|---|---|
| `permission-bash.json` | Common input fields; PermissionRequest (`tool_name` `Bash`, `tool_input.command`, `tool_input.description`) | permission prompt for a shell command |
| `permission-apply-patch.json` | PermissionRequest (`apply_patch` uses `tool_input.command`; `description` null) | permission prompt for a file change |
| `permission-mcp-tool.json` | PermissionRequest (MCP names `mcp__<server>__<tool>`, arguments as `tool_input`) | permission prompt for an MCP tool |
| `stop-first.json` | Stop (`stop_hook_active` false, `last_assistant_message`) | a turn's first Stop |
| `stop-continued.json` | Stop (`stop_hook_active` true: "already continued by Stop") | Stop of a turn the hook continued |
| `prompt-typed.json` | UserPromptSubmit (`prompt`) | the human typing a prompt |

Answers the adapter writes, from the same page: PermissionRequest
`{"hookSpecificOutput":{"hookEventName":"PermissionRequest","decision":{"behavior":"allow"|"deny","message":...}}}`;
Stop `{"decision":"block","reason":"..."}` (the reason becomes a continuation prompt).

## What was checked against the real codex-cli 0.170.0 (no sign-in needed)

- `CODEX_HOME` sets the configuration folder (`codex app-server` `initialize` answers with
  `codexHome`). Every command below used a temporary one, never `~/.codex`.
- Inline `[mcp_servers.pong]` with `tool_timeout_sec` is read: `codex mcp get pong --json` shows it
  (tested in `install_test.go`).
- Inline `[[hooks.<Event>]]` / `[[hooks.<Event>.hooks]]` with `type`, `command`, `timeout` is
  read; no `matcher` means `matcher: null`, all tools. `codex features list` shows `hooks` stable
  and on, so no feature flag is written.
- Trust: `codex app-server`, request `hooks/list` (`{"cwds":[...]}`), lists each hook with
  `trustStatus` one of `managed`, `untrusted`, `trusted`, `modified` and a `currentHash`. Newly
  added hooks are `untrusted`. Status reads this. The trust itself appears to be stored under a
  `hooks.state` key in config.toml (seen in the binary's strings only; not confirmed).
- Login lives in `$CODEX_HOME/auth.json` unless `cli_auth_credentials_store` says otherwise (from
  the binary's strings and `codex login --help`; the file was not read). So a temporary
  CODEX_HOME does not see the real login unless that store is the keyring.

## The app-server handshake as seen without sign-in

`codex app-server` speaks JSON lines on stdio (no `Content-Length` framing; `"jsonrpc":"2.0"` is
accepted). Sent `{"id":1,"method":"initialize","params":{"clientInfo":{"name":"probe","version":"0"}}}`
then `{"method":"initialized"}`. It answered:

    {"id":1,"result":{"userAgent":"probe/0.170.0 (Mac OS 26.6.0; arm64) unknown (probe; 0)","codexHome":"<CODEX_HOME>","platformFamily":"unix","platformOs":"macos"}}
    {"method":"remoteControl/status/changed","params":{"status":"disabled","serverName":"<host>","installationId":"<uuid>","environmentId":null},"emittedAtMs":<ms>}

`codex app-server generate-json-schema --out DIR` writes the full protocol schema. Running the app
server creates Codex state files (sqlite databases, `installation_id`) in CODEX_HOME.

## Recording procedure (after `codex login`)

Run in a scratch folder outside the repository, with a temporary Codex home that has the login:

    export CODEX_HOME=$(mktemp -d)       # then make the login available there: run `codex login`
                                         # with this CODEX_HOME set (do not copy auth files)
    mkdir -p /tmp/codex-rec && cd /tmp/codex-rec && git init -q

1. Write `$CODEX_HOME/config.toml` registering one recording command hook on PermissionRequest,
   Stop, UserPromptSubmit, SessionStart and SessionEnd (and PreToolUse, PostModelRequest for
   reference), e.g. `command = "/bin/sh -c 'cat >> /tmp/codex-rec/$0.jsonl' EVENT"` per event. For
   the Stop hook, also print `{"decision":"block","reason":"Reply with the word CONTINUED"}` the
   first time (a marker file) to record the continued turn's Stop.
2. Start `codex` in that folder, run `/hooks` and trust every hook (this confirms the trust step;
   then check `hooks/list` shows `trusted`, and record which config.toml key changed).
3. Prompts, with the default approval policy so Codex asks: (a) "Run `date > stamp.txt`" — approve
   in Codex; (b) "Create notes.txt containing hello" (apply_patch); (c) an MCP tool call to a tiny
   stdio server added under `[mcp_servers.host]`; (d) "Reply with exactly OK" for Stop and
   continuation; then `/exit` for SessionEnd.
4. Copy single lines into fixtures, replacing the documented ones: PermissionRequest Bash →
   `permission-bash.json`, apply_patch → `permission-apply-patch.json`, MCP → `permission-mcp-tool.json`;
   the first Stop → `stop-first.json`, the continued one → `stop-continued.json`; UserPromptSubmit →
   `prompt-typed.json`. Keep raw `*.jsonl` under `testdata/recorded/`, move this folder's files
   there, and rewrite this README as a recording README with the Codex version.
5. Check the answers: point the hooks at `pong hook codex` (with an Engine running), answer a
   permission Ping Allow and Deny in Pong and confirm Codex obeys, Pong a finished Ping and confirm
   the turn continues, and note whether a Stop hook waiting for a long time blocks the terminal
   (Codex documents only synchronous Stop hooks as able to continue a turn).
6. Fix any difference, run `go test ./internal/harness/codex`, add the version to `TestedVersions`.
