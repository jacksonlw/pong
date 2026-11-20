# Fixtures

- `events.sse` — Pings recorded from a real Engine with `GET /events?since=0` (card snapshots).
- `context.sse` — Projects, Sessions and Pings recorded from a real Engine (`PUT /sessions`,
  `POST /pings` with `context`, `why` and answers' `leads_to` / `hard_to_undo`, then
  `GET /events?since=0`), for the Project line, the context beside the card and the ring.

  **Hand-added:** the `context` object on the `session.changed` lines for
  `s-migrate` and `s-release` was added by hand, because the Engine does not report
  context-window figures yet. Its shape is the one the app decodes:

      "context": {"used_fraction": 0.42,   // 0…1, how full the window is
                  "tokens": 420000,        // tokens used
                  "window": 1000000,       // the window's size in tokens
                  "cost_usd": 3.18,        // the Session's cost so far
                  "model": "claude-opus-4-5"}

  Every part is optional; the ring shows only when `used_fraction` is present. `s-flaky` has no
  figures. Once the Engine reports them, re-record this file from a real Engine and delete this note.
- `more-coming.sse` — recorded from a real Engine with `pong ping -more-coming` and `pong pong`:
  `s-steps` asks one Ping with more coming, it is answered, then its last Ping arrives;
  `s-asking` has one waiting Ping with more coming. For the typing badge and "being written".
- `tags.sse` — recorded from a real Engine with `GET /events?since=0` after `pong ping -folder /Users/me/shop -tag …`
  from five Sessions: `s-none` (no Tags), `s-neutral` (one neutral Tag), `s-coloured` (`prod` in rose),
  `s-unknown` (an icon outside the vocabulary) and `s-many` (eight Tags, more than fit). For the Tag row.
- `settings/` — for the Settings window: `projects.json`, `settings.json` and `harnesses.json`
  are `GET /projects`, `GET /settings` and `GET /harnesses` from a real Engine on a temporary
  data directory, run with `HOME` set to a temporary home holding empty `.claude` and `.codex`
  folders, after `pong setup codex` into that home (so Codex is connected with its hooks not yet
  trusted, Claude Code installed but not connected). Two Projects came from `pong ping -note`
  in folders `shop` and `billing-api`; `shop-logo.png` was sent with `PUT /projects/{id}/logo`
  and is what its logo path returns.
- `start/` — for the start panel in the Bar: `projects.json` and `harnesses.json` are
  `GET /projects` and `GET /harnesses` from a real Engine on a temporary data directory,
  run with `HOME` set to a temporary home with empty `.claude` and `.codex` folders. Projects `shop`
  and `billing-api` came from `pong ping -note`; `PUT /sessions` then registered a Codex Session in
  `shop` and a Claude Code Session in `billing-api` (the Harness last used in each). `shop-logo.png`
  was sent with `PUT /projects/{id}/logo`. `start-refused.json` is the 422 body of
  `POST /sessions/start` with Codex in `shop`.
- `thread/` — `GET /thread` for `s-new` and `s-old` from a real Engine on a temporary data directory: `pong ping`/`pong pong` (an answer with text, ticked answers, a Note, a Resource, one left waiting), `PUT /sessions` giving `s-new` its name and `continued_from` `s-old`; the temp path was replaced with `/Users/me`.
  `s-many.json` is `GET /thread` for `s-many` from the Engine that recorded `tags.sse`.
  `s-thoughts.json` is `GET /thread` for `s-thoughts` from a real Engine: a question and its Pong, two Thoughts, then a finished Ping. The Pong's `delivery` was set to `delivered` by hand, since no Harness took it in that run.
- `shape/` — Pings that say their shape, **written by hand** while the Engine side was being
  built, in the wire shape it gives: `sheet.json` is a sheet
  Resource's content (`application/vnd.pong.sheet+json`, two sheets) and `link.txt` a link
  Resource's (`text/uri-list`). The Pings that carry them, with `finished`, are built in
  `PingShapeTests.swift`, and the two-page slides PDF is drawn there. Once the Engine lands,
  re-record them from a real Engine and delete this note.
