# Codex app-server fixtures: written from the published schema, NOT recorded

Every `*.json` in this folder was **written by hand** on 2026-11-18 from the JSON schema that
codex-cli 0.170.0 publishes about itself (`codex app-server generate-json-schema --out DIR`, run
with a temporary `CODEX_HOME`). The schema files used are copied, unchanged, into
`schema-0.170.0/`. None of these messages was recorded: Codex was not signed in, so no turn could
run and no approval, question or completed turn could be seen. **The Runner's behaviour on these
messages is unverified against a real, signed-in Codex.** Ids, paths and texts are placeholders;
the fake (`../../fakecodex`) fills in thread and turn ids.

| File | Schema used | What it stands for |
|---|---|---|
| `command-approval.json` | `ServerRequest` `item/commandExecution/requestApproval`, `CommandExecutionRequestApprovalParams` | approval of a shell command; answered `{"decision":"accept"|"decline"}` |
| `file-change-started.json` | `ItemStartedNotification`, `ThreadItem` `fileChange` (`changes[].path/kind/diff`) | the file change item the approval refers to |
| `file-change-approval.json` | `item/fileChange/requestApproval`, `FileChangeRequestApprovalParams` | approval of a patch; answered `{"decision":"accept"|"decline"}` |
| `question.json` | `item/tool/requestUserInput`, `ToolRequestUserInputParams` (experimental; needs `capabilities.experimentalApi`) | a structured question; answered `{"answers":{"<id>":{"answers":[…]}}}` |
| `agent-message.json` | `ItemCompletedNotification`, `ThreadItem` `agentMessage` | the agent's last message in a turn |
| `turn-completed.json` | `TurnCompletedNotification` | the end of a turn |
| `account-signed-in.json` | `GetAccountResponse` (`account` a `ChatgptAccount`) | `account/read` when signed in (the signed-out answer is recorded) |

What *was* seen on the real 0.170.0 is under `../../recorded/app-server-signed-out/`.

## After sign-in: verify and record

Use a temporary Codex home, never `~/.codex`, and a scratch folder outside the repository:

    export CODEX_HOME=$(mktemp -d)
    codex login                      # the human signs in, with this CODEX_HOME set
    mkdir -p /tmp/codex-rec && cd /tmp/codex-rec && git init -q

1. Record a session as the Runner drives it: a small script (Python or `go run`) that starts
   `codex app-server --disable hooks`, sends `initialize` (with `capabilities.experimentalApi`
   true), `initialized`, `account/read`, `thread/start {"cwd":"/tmp/codex-rec"}`, then
   `turn/start` with, in turn: (a) "Run `date > stamp.txt`" (answer the approval `accept`);
   (b) "Create notes.txt containing hello" (a patch; answer `decline`, then once more `accept`);
   (c) a prompt that makes Codex ask a structured question (request_user_input; may need Plan
   mode or a collaboration mode — note what triggers it); (d) "Reply with exactly OK". Log every
   line both ways to `client.jsonl` / `server.jsonl`.
2. Replace the fixtures here with single recorded lines: the real
   `item/commandExecution/requestApproval` → `command-approval.json`; the `item/started` of the
   `fileChange` item → `file-change-started.json`; `item/fileChange/requestApproval` →
   `file-change-approval.json`; `item/tool/requestUserInput` → `question.json`; the last
   `item/completed` `agentMessage` → `agent-message.json`; `turn/completed` →
   `turn-completed.json`; `account/read` → `account-signed-in.json` (replace the email). Put the
   raw logs in `../../recorded/app-server-signed-in/`, scrubbing paths, host and account details,
   and move this folder's files there with a recording README naming the Codex version.
3. Check against the real Codex what the fixtures cannot show: that approvals reach the client
   with the default approval policy; that `decline` lets the turn carry on; whether Pong's
   installed hooks fire inside the app server (the Runner passes `--disable hooks` assuming they
   would); that `thread/resume` then `turn/start` continues the thread; and that
   `codex resume <id>` opens the thread in a terminal.
4. Run `go test ./internal/harness/codex`, fix any difference, then start a real Session through a
   running Engine (`POST /sessions/start {"harness":"codex",…}`) and answer each kind of Ping in
   Pong.
