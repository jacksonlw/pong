# Status line fixtures: written from the documentation, NOT recorded

These files were **written by hand from Claude Code's documentation**, "Customize your status
line" (https://code.claude.com/docs/en/statusline, "Available data" and its full JSON schema),
read on 2026-11-18. They are not recorded: Claude Code does not run the status line in `-p` mode,
so recording one needs a person at an interactive terminal (steps in `../README.md`).

- `input.json` — the documented example, cut to the fields Pong reads and their neighbours.
- `early.json` — the same shape early in a Session, as the documentation describes it:
  `used_percentage` and `current_usage` are `null` before the first API response.
