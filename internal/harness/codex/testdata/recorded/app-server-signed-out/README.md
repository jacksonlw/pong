# Codex app server, signed out: recorded

Recorded 2026-11-18 from the real **codex-cli 0.170.0** (`codex app-server --disable hooks`,
stdio, JSON lines) with a temporary, empty `CODEX_HOME` and no sign-in. `client.jsonl` is what was
sent, `server.jsonl` everything the app server wrote, in order. Only these were replaced: the
scratch folder path by `/tmp/proj`, the Codex home by `<CODEX_HOME>`, the host name by `<host>`
and the installation id by `<uuid>`.

What it shows: the `initialize` answer; `account/read` signed out
(`{"account":null,"requiresOpenaiAuth":true}`, which the Runner's Check reads as not signed in);
`thread/start` succeeding without a sign-in (with its thread id, approval policy `on-request`);
`turn/start` accepted, then the turn retrying on HTTP 401 (`error` notifications with
`willRetry: true`). The recording stops before the turn gave up. The fake codex answers
`initialize`, `account/read` (signed out), `thread/start`, `thread/resume` and `turn/start` from
these lines.
