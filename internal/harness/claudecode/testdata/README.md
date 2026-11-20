# Claude Code fixtures

Every `*.json` payload here that is not `*.out.json` was recorded from the real Claude Code
**2.1.322** on macOS, 2026-11-18. None is invented or edited; each is one line of hook stdin
exactly as Claude Code sent it. `*.out.json` files are the bytes the adapter is expected to write
back for a given Pong (written for the tests, not recorded); their shapes match the hook outputs
Claude Code 2.1.322 accepted in the recording runs (`recorded/*/hook-outputs-sent.jsonl`).

- `question/single.json` — PreToolUse for AskUserQuestion, one question.
- `question/several.json` — PreToolUse for AskUserQuestion, three questions, the second multi-select.
- `question/repeats-labels.json` — PreToolUse for AskUserQuestion whose question names its
  answers ("Postgres or SQLite?"), recorded in the permission-decisions run below.
- `permission/bash.json`, `permission/write.json`, `permission/mcp-tool.json` — PermissionRequest
  for Bash, Write and `mcp__host__echo`, each with `permission_suggestions`
  (from `recorded/permission-decisions/`).
- `plan/exit-plan-mode.json` — PreToolUse for ExitPlanMode in plan mode (Claude Code injects
  `plan` and `planFilePath`).

- `stop/first.json`, `prompt/typed.json` — copies of `recorded/ask-and-permissions/Stop.jsonl` and
  `UserPromptSubmit.jsonl` (a turn's first Stop: `stop_hook_active` false).
- `stop/woken.json`, `prompt/woken.json` — from an interactive run (below): the UserPromptSubmit
  Claude Code sends for a turn woken by an asyncRewake Stop hook (its `prompt` is a
  `<task-notification>` carrying the hook's standard error), and that turn's Stop, with
  `stop_hook_active` true.

## Interactive run

`recorded/interactive/` holds the rest of that run. Claude Code 2.1.322, interactive, driven by
`expect` in a folder under an already trusted one, with
`claude --settings settings.json --setting-sources local --no-chrome --model haiku`. The settings
registered on Stop a plain command hook and an `asyncRewake: true` (`timeout` 600) command hook,
plus UserPromptSubmit and SessionEnd recorders. Prompt: "Reply with exactly the word OK". Verified:

- Both Stop hooks get the same payload. The asyncRewake one ran in the background: the prompt
  was usable at once, and 8 s later, with the Session idle, it exited 2 and its standard error
  woke the Session as a new turn ("Stop hook blocking error from command ..."), which answered.
- That woken turn fires UserPromptSubmit (`prompt` is the task-notification) and its Stop has
  `stop_hook_active: true`.
- On `/exit`, an asyncRewake Stop hook still running gets SIGTERM; SessionEnd then fires with
  `reason: "prompt_input_exit"` (`-p` runs record `"other"`).

## How they were recorded

In a scratch folder outside the repository, with no change to `~/.claude`:

    claude -p --settings settings.json --mcp-config mcp.json \
      --permission-prompt-tool mcp__host__approve --model haiku "<prompt>"

- `settings.json` registered one command hook on PreToolUse, PermissionRequest, PostToolUse,
  Stop, SessionStart, SessionEnd, UserPromptSubmit and Notification (matcher `*`). The script
  appended its stdin to `<event>.jsonl`; for AskUserQuestion and ExitPlanMode it also printed an
  answer, to check which output shape Claude Code takes.
- `mcp.json` ran a ten-line stdio MCP server whose `approve` tool always denies, used as
  `--permission-prompt-tool`. **This is needed:** in `-p` mode Claude Code 2.1.322 does not offer
  AskUserQuestion or ExitPlanMode at all without a permission host (checked: without one the agent
  reports the tool is unavailable and no hook fires). The server's own tool `echo` was the MCP tool
  whose permission prompt was recorded.
- Prompts: (a) ask one question, then one AskUserQuestion with three questions (one multi-select),
  then run `date > stamp.txt`, Write `notes.txt`, call `mcp__host__echo`; (b) in
  `--permission-mode plan`, plan adding `hello.txt` and present it; the hook denied the first
  ExitPlanMode with feedback and allowed the revised one.

Paths in the payloads (home folder, scratch folder, transcript path) are left as recorded.

## recorded/

Raw payloads from the same two runs, for the tickets after 07 (no handlers here):
`recorded/ask-and-permissions/` and `recorded/plan-mode/`, one `<event>.jsonl` per hook event.

- `PermissionRequest.jsonl` — Bash `date > stamp.txt`, Write `notes.txt`, MCP `mcp__host__echo`.
  Each carries `permission_suggestions` (addDirectories, setMode, addRules).
- `PreToolUse.jsonl`, `PostToolUse.jsonl` — the same tool calls before/after (plan mode also shows
  Claude writing its plan file, and ToolSearch loading ExitPlanMode).
- `Stop.jsonl`, `SessionStart.jsonl`, `SessionEnd.jsonl`, `UserPromptSubmit.jsonl` — one each per run.
- `permission-prompt-tool-calls.jsonl` — what the permission host received.
- `hook-outputs-sent.jsonl` — the answers the recording hook printed, which Claude Code accepted.

Not captured: **Notification** (does not fire in `-p`), and **status line** input (Claude Code
does not run the status line in `-p`). To record them a human runs `claude --settings <file>` with
the same recording hook and a `statusLine` command that appends stdin to a file, in an interactive
terminal, and waits about a minute at a prompt for `idle_prompt`.

## context/

`context/transcript.jsonl` is the transcript (`transcript_path`) of the ask-and-permissions run
above, Claude Code **2.1.322**, **trimmed**: only the `user` and `assistant` entries are kept, each
cut to `type`, `isSidechain`, ids, `version`, `timestamp` and `message.{model,id,type,role,
stop_reason,usage}`, with `message.content` emptied (private content removed). The usage numbers
are as recorded.

What Claude Code 2.1.322 exposes to a hook, checked against that version:
- Hook input has no usage, cost or window fields; it names the transcript.
- Each `assistant` transcript entry states `message.model` and `message.usage`
  (`input_tokens`, `cache_creation_input_tokens`, `cache_read_input_tokens`, `output_tokens`).
  At PreToolUse time the entry of the response that called the tool is not yet written, so the
  figures are those of the last response already in the transcript (none early in the first turn).
- A `cost-state` entry (`totalCostUSD`, `modelUsage`) is written when the process ends, so it does
  not describe the moment of asking; not used. No entry states the window size.
- The status line input (`context_window.context_window_size`, `used_percentage`,
  `cost.total_cost_usd`) is the only source of window, fraction and cost. The status line command
  does not run in `-p` mode (checked: a `statusLine` in `--settings` was never called), so it was
  not recorded. **Outstanding, for a human:** in an interactive `claude` 2.1.322 session, set
  `statusLine` to a command that appends its stdin to a file, send two prompts, and add that file
  here; only then can those three figures be read (the status line would also have to hand them to
  Pong, since hooks cannot see it).

## recorded/permission-decisions/

A third run (2026-11-18, same Claude Code 2.1.322) to check which PermissionRequest answers Claude
Code takes. Same recording hook, but **without** `--permission-prompt-tool` (with it, that tool's
answer wins over the hook's: every call was denied although the hook allowed it). The hook
answered Bash with `{"behavior":"allow"}` (the command ran), Write with `{"behavior":"deny",
"message":...}` (refused; Claude read the message and retried with another name, denied again),
and the MCP tool with `{"behavior":"allow","updatedPermissions":<its permission_suggestions>}` (the
tool ran and Claude Code wrote `mcp__host__echo` to the scratch project's
`.claude/settings.local.json`). `hook-outputs-sent.jsonl` holds the answers; the question fixture
came from an earlier attempt of this run made with the permission host, where AskUserQuestion is
offered.

## statusline/

Written from Claude Code's status line documentation, **not recorded**; see `statusline/README.md`.
**Outstanding, for a human**, with Pong installed and the Engine and
Mac app running:

1. In a scratch folder, write `record.sh`: `#!/bin/sh` then `tee -a "$HOME/statusline-input.jsonl" | pong statusline claude-code`, and `chmod +x record.sh`.
2. Run `claude --settings '{"statusLine":{"type":"command","command":"/full/path/to/record.sh"}}'`
   in an interactive terminal, accept the trust dialog, send two prompts, then quit.
3. Check that the Session's context ring appears in the Mac app with the window, percentage and
   cost Claude Code shows.
4. Copy one line of `~/statusline-input.jsonl` (the second prompt's) to
   `statusline/recorded.json`, note the Claude Code version here, and delete the `.jsonl` file.

## Live checks (glue, 2026-11-18)

Claude Code **2.1.322**, `-p --model haiku`, with the plugin generated by `WriteMarketplace` into a
scratch folder and loaded with `--plugin-dir <dir>/plugin` (not installed; `~/.claude` untouched;
`--setting-sources local`), a real Engine on a temporary `PONG_HOME` and a non-default port.

- `tools/send-ping.json` is the PreToolUse payload Claude Code sent for the agent's call of Pong's
  tool. The plugin's MCP tools are named `mcp__plugin_pong_pong__<tool>` (`mcp_server.name`
  `plugin:pong:pong`).
- `tools/send-thought.json` is not a recording: it is `tools/send-ping.json` with `tool_name` set to
  `mcp__plugin_pong_pong__send_thought` and a Thought as `tool_input`. A headless run now
  gets no Pong tools, so the call could not be recorded this way; a real Session sending a Thought
  is on the manual checklist.
- SessionStart registered the Session and the agent said it had received the standing instruction.
- The PreToolUse hook on `mcp__plugin_pong_pong__.*` answered `allow` with `updatedInput` adding
  `session`, `folder` and `harness`: the tool ran without a permission prompt (in `-p` an unallowed
  MCP tool would be refused), and the Ping carried the Claude Code Session id and
  `harness: claude-code`. The Stop hook then waited on that Ping (no second, plain Ping was made).
- In `-p` the process does not exit while the asyncRewake Stop hook waits: a Pong sent with
  `pong pong <id> -answer Yes -text ...` woke the Session as a new turn, the agent did what the
  text said, its Stop (`stop_hook_active` true) found the Ping answered, and the process exited.
- PermissionRequest (Bash `date > stamp.txt`, no `--permission-prompt-tool`) arrived as a blocking
  Ping with answers Allow / Always allow / Deny; the Pong Allow let the command run.
- Findings: in that second run the agent sent its finished Ping on its own, with no answers, so
  "Nothing more" was not offered and could not be chosen; a Pong with text was needed to end it.
  The woken turn in `-p` did not move `turn_started_at` (no UserPromptSubmit seen for it).
- Not checked live: AskUserQuestion / ExitPlanMode through the plugin (in `-p` they need a
  permission host, whose answer then wins over the PermissionRequest hook; see above), the nudge
  and the plain finished Ping, and SIGTERM on exit (all covered by earlier recordings).

## Sessions the Engine starts

`auth/signed-out.json` is `claude auth status --json` from Claude Code 2.1.322 with an empty
`CLAUDE_CONFIG_DIR` (exit status 1), with the two folder paths dropped; `auth/signed-in.json` is
the signed-in answer cut to its non-private fields (exit status 0). `fakeclaude/` is the fake
`claude` the Runner tests put on the captured PATH; its doc comment says how a test drives it.

Checked on the real Claude Code 2.1.322 (`claude -p`, `--model haiku`, Pong's plugin loaded with
`--plugin-dir` from a copy written into a scratch folder, Stop registered with `asyncRewake`; no
change to `~/.claude`), first by hand against a scratch Engine and then through the Engine's own
`POST /sessions/start` with a wrapper `claude` that added `--plugin-dir`:

- In `-p` mode the process does not exit while a Stop hook runs, async or not. A Stop hook that
  exits 2 makes Claude Code carry on with its standard error as the next turn in the same process
  ("Stop hook feedback"), both for the nudge and for a Pong arriving minutes later (checked with a
  6-second hook, async and plain, and with Pong's hook: the Pong "reply with exactly the word AGAIN"
  was answered `AGAIN` in the same process, one `cost-state` in the transcript).
- If the Pong arrives before the Stop hook starts waiting (the agent's `send_ping` hold has run
  out but its turn has not ended), the hook finds the Ping answered and exits 0, and the process
  exits. The Engine then resumes the Session with that Pong (`claude -p --resume <id>`, prompt on
  standard input): checked, two `cost-state` entries, `AGAIN` answered once.
- Without a permission host, permission prompts in `-p` reach the PermissionRequest hook (the
  agent's first `mcp__plugin_pong_pong__send_ping` arrived as an "Allow …?" Ping); hence
  `--allowedTools mcp__plugin_pong_pong`. AskUserQuestion and ExitPlanMode are not offered (also
  with `--input-format stream-json`), so the agent asks with Pong's tools.
- `--allowedTools` takes several values: a prompt given after it as an argument is swallowed, so
  the prompt goes on standard input.
- Pong's MCP server takes its Session from `PONG_SESSION`, which the Runner sets to the Session id
  it passes in `--session-id`; the agent's own Pings then join the Session (with no Harness named).
  A `claude` that the human's own hooks start inherits it too (seen with a user Stop hook that runs
  `claude -p`): its Pings land in the same Session.
- Not signed in: `claude auth status` exits 1 with `"loggedIn": false`, and `claude -p` prints
  `Not logged in · Please run /login` and exits 1.

## Auto mode refusals

`denied/bash.json` is the PermissionDenied payload Claude Code **2.1.322** sent on 2026-11-19 when
auto mode refused `git push --force origin main` in a scratch repository (`-p --permission-mode
auto --model haiku --setting-sources local`, the command named in a file the agent read, so no
typed message asked for it). Checked in that run and in interactive ones driven by `expect`:

- Auto mode's classifier reads typed messages and tool calls, never tool results: a go-ahead given
  in a Pong does not clear a refusal.
- `retry: true` is the only output Claude Code reads from a PermissionDenied hook. The agent then
  gets one line, "The PermissionDenied hook indicated you may retry this tool call." Without more,
  haiku left the refusal standing or asked its human again, so the SessionStart instruction says
  what that line means (`RetryInstruction`). `additionalContext` on this event reaches nobody.
- The agent sends the command again with a new `description`.
- Interactive, auto mode: a PreToolUse hook answering `ask` sends the call to the permission
  prompt, PermissionRequest fires (`permission_mode` still `auto`), and its `allow` runs the call
  ("Allowed by PermissionRequest hook") with no classifier verdict.
- `-p`, auto mode: the same `ask` is refused outright with the hook's reason and PermissionRequest
  does not fire.
- End to end with the plugin from `WriteMarketplace` (`--plugin-dir`), a scratch Engine and
  `pong pong <id> -answer Allow`: the refused force push ran on the agent's retry.

Not recorded as payloads, read from the transcripts of real Sessions in the Claude app (Claude
Code 2.1.326, `entrypoint` `claude-desktop`, auto mode, 2026-11-20) and the Engine's diagnostics
record. After an Allow in Pong, every retried Bash and Agent call that PreToolUse answered `ask`
ran. A retried `mcp__ccd_session__start_session` (three times, once with `proposal_id`) and a
retried Edit were refused again by the classifier, with the same reason and no PermissionRequest,
and PermissionDenied fired again for the same call. So auto mode can settle a call itself even
after `ask`, and Pong cannot allow it. The tests for this (`TestNoSecondRetryAfterRefusedAgain`)
take `denied/bash.json` and swap in the tool and input of those calls.

## Rechecked on 2.1.328 (2026-11-19)

Claude Code updated itself to **2.1.328**. The runs above were made again on it, in scratch
folders with the same kind of recording hook and permission host, and each payload's fields were
compared with the 2.1.322 recording of the same event and tool. The fixtures here stay the
2.1.322 ones; nothing was replaced.

- Same fields, and the same fields in `tool_input`, for: PreToolUse (AskUserQuestion with one and
  with three questions, ExitPlanMode, Bash, Write, an MCP tool, ToolSearch), PermissionRequest
  (Bash, Write, an MCP tool, each with `permission_suggestions`), PostToolUse, UserPromptSubmit
  (typed, and for a turn woken by an asyncRewake Stop hook), Stop (first, and woken with
  `stop_hook_active` true), SessionStart, SessionEnd (`prompt_input_exit` on `/exit`) and
  PermissionDenied (auto mode refused `git push --force origin main` named in a file, with the
  same `reason`).
- One field differs: `scratchpad_dir` was in every 2.1.322 payload and in none of these. The
  2.1.322 runs were made from inside a Claude Code session and these with a clean environment,
  which may be the whole difference. Pong does not read it.
- The same answers were accepted: AskUserQuestion's `allow` with `updatedInput.answers` (the
  answers came back in `tool_response`), ExitPlanMode's `deny` with a reason and then `allow`,
  and PermissionRequest's `allow` (the command ran), `deny` with a message (the file was not
  written) and `allow` with `updatedPermissions` (the rule was written to the scratch project's
  `.claude/settings.local.json`).
- An asyncRewake Stop hook that exited 2 after 8 seconds woke the idle interactive Session as a
  new turn, as before.
- With Pong's own plugin installed: a Session started in a terminal sent its finished Ping
  through `mcp__plugin_pong_pong__send_ping` under its own Session id, text written in its Thread
  reached it through PreToolUse `additionalContext` and through the Stop hook, and
  `claude auth status` and `--session-id`, `--resume` and `--remote-control` with a prompt after
  `--` behaved as the Runner expects.
- Not made again: the PermissionDenied `retry` answer and the `ask` that follows it, and anything
  about the status line.

## Turns nobody typed (2026-11-19)

`prompt/subagent-report.json` is the UserPromptSubmit payload Claude Code **2.1.326** (the build
the Claude app runs, driven as the app drives it: `-p --input-format stream-json --output-format
stream-json --verbose`, `--setting-sources local`, `--model haiku`) sent when a subagent started
with the Agent tool's `run_in_background` finished after its Session's turn had ended. Recorded in
a scratch folder with a recording hook on UserPromptSubmit and Stop and a second, `asyncRewake`
Stop hook that slept 40 seconds and exited 2. Checked in that run:

- The report starts a new turn and fires UserPromptSubmit with the same fields as a typed prompt.
  Only `prompt` differs: it is the `<task-notification>` (`<task-id>`, `<status>`, `<result>`).
  Nothing in the payload says who submitted it.
- That turn's Stop has `stop_hook_active` false.
- The first turn's asyncRewake Stop hook was not stopped when the report's turn began. It ran
  on beside the second turn's Stop hook, and each woke the Session when it exited 2.
- Claude Code 2.1.329 from a terminal's `claude -p` sent the same payload for the report, but
  only with no asyncRewake Stop hook running: with one, the report waited for the hook and
  started no turn.

Not recorded: the same report in the Claude app itself, where it arrives as a message that opens
`<agent-message from="...">` ("[Subagent hand-back]"). The transcript of a real Session there
(2026-11-20T02:05:18Z) shows that message queued and a turn begun with it, and Pong's
UserPromptSubmit hook ran for it. In both recorded runs the queued text and the hook's `prompt`
were the same, so the hook's `prompt` for a hand-back is taken to open with that tag.

## A hook that stops a turn

Claude Code **2.1.329**, in scratch folders with a recording hook on every event, `--setting-sources
local`, `--model haiku`. The hook answered one PreToolUse with a stop and nothing else.
`recorded/hook-stop/` holds each run's events and `hook-outputs-sent.jsonl`, the answer the hook
printed. `stopped/bash.json` and `stopped/send-ping.json` are the two PreToolUse payloads that were
answered with a stop in the interactive runs.

**The turn halts.** In every run the agent made no further model call after the answered hook.

- `continue-only/` (`-p --allowedTools Bash`, three `date > N.txt` commands asked for, the second
  answered `{"continue": false, "stopReason": "Your human stopped this turn in Pong."}`): the
  turn ended, but **the answered tool call still ran**. `two.txt` was written and PostToolUse
  fired for it. The result line said `terminal_reason: "hook_stopped"`, `subtype: "success"`, with
  an empty `result`; the stop reason was not in the stream output.
- `deny-and-continue/` (the same run, the hook also denying:
  `{"continue": false, "stopReason": R, "hookSpecificOutput": {"hookEventName": "PreToolUse",
  "permissionDecision": "deny", "permissionDecisionReason": R}}`): the tool call did not run
  (no `two.txt`, no PostToolUse; its result was the error "PreToolUse:Bash hook error: R") and
  the turn ended the same way. **This is the answer that stops at the tool call.**
- `interactive/` (the deny-and-continue answer, in a terminal driven by `expect`): the terminal
  showed "1 action not run · permission denied │ Reason: PreToolUse:Bash hook error: Your human
  stopped this turn in Pong." and under it "PreToolUse:Bash hook stopped Claude: Your human
  stopped this turn in Pong.", then the prompt. The agent wrote nothing more. The Session stayed
  open: a prompt typed next ("Reply with exactly the word OK") was answered in it, with its own
  UserPromptSubmit and Stop.
- `own-tool/` (interactive, a scratch plugin named `pong` whose MCP server was the built
  `pong mcp` against a scratch Engine; the agent ran one shell command, loaded the tool with
  ToolSearch, then called `mcp__plugin_pong_pong__send_ping`, which was answered with the
  deny-and-continue answer): the tool was not called, the scratch Engine had no Ping, the turn
  ended and the Session stayed open, as for Bash.

**The Stop hook does not fire for a turn a hook stopped,** in `-p` or in a terminal. No run has a
Stop event for the stopped turn; `-p` went straight to SessionEnd (`reason: "other"`). So nothing
of Pong's runs at the end of a stopped turn: no nudge, no Ping from the last message, and no
asyncRewake hook left listening for text.

- `woken/` checks one way to listen all the same: a second PreToolUse hook registered with
  `asyncRewake: true` ran in the background beside each tool call, and when it exited 2 ten
  seconds after the turn was stopped, its standard error woke the idle Session as a new turn
  (UserPromptSubmit with a `<task-notification>` whose reminder reads `Stop hook blocking error
  from command "PreToolUse:Bash": …`; that turn's Stop has `stop_hook_active` true), and the
  agent did what the text said.

Live check (2026-11-19, Claude Code 2.1.329 in a terminal driven by `expect`, the
plugin's own hooks from `integrations/claude-code/plugin` loaded with `--plugin-dir`, a scratch
Engine on `PONG_HOME`): three `sleep 10 && date > N.txt` commands were asked for and
`POST /session/stop` was sent during the first. The second tool call was not run, the terminal
showed the stop, the Session was `idle` with its stop `stopped`, and the Thread had its line.
Text then sent with `POST /session/follow-up` woke the Session through the background
PreToolUse hook, and the agent did what it said.

Not recorded: a stop answered while several tool calls of one response were waiting, and a stop
in a Session moved into the Claude app.
