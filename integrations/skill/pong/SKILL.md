---
name: pong
description: How to write a good Ping with Pong's tools (send_ping, wait_for_pong, send_note, send_thought, withdraw_ping). Use before asking your human a question, asking them to approve a plan or action, or handing over finished work.
---

# Writing a Ping

Your human sees each Ping on its own, often away from this Session. Write it so they can answer in seconds without opening your Session.

## What shape it is

Every Ping says what it is:

- a question or an approval sets `blocks_agent`;
- finished work sets `finished: true`, and never `blocks_agent`;
- news that needs no answer is a Note, sent with `send_note`.

Pong refuses a Ping that is none of these, and one that is both finished and blocking.

## The question

- One question per Ping. With several questions, send several Pings, one at a time, and set `more_coming` on each one that another will follow.
- One or two lines that carry their own context: name the file, feature or decision. "Which tables should the migration touch?" not "Which ones?"
- Never restate the answers in the question. The answers are shown under it; Pong refuses a question that repeats an answer's label.
- Put background in `context` and the reason you are asking now in `why`: what you found and why it matters, so the human can decide without your Session. Keep each to a short paragraph; anything longer goes into a document Resource.
- Say it, never point to it. "The Thought explains the test choice" or "see the plan above" tells the human nothing: your Thoughts, your messages and your Session's closing text are not on the card.
- With several questions in a row, such as a round of design questions, each Ping still carries its own `context` and your recommendation.

## Answers

- At most four. If there are more options, merge them or ask a narrower question.
- Keep each label as short as the answer allows: five words or fewer when that says it, such as "Use SQLite". When an answer cannot be said that briefly, such as a value to confirm or a choice that needs its condition, the label may run to twelve words. Pong refuses a longer one.
- Say in `leads_to` what choosing it leads to, never in the label, and set `hard_to_undo` when it is.
- Put your recommendation first and name it in `recommended`. Nothing else marks it; do not write "(recommended)" in the label.
- Set `allow_several` when the human may tick more than one. Leave answers out when you need typed text.
- To ask for approval of a plan or an action, set `approves` to `plan` or `action`.
- A plan to approve always comes as a document Resource, with `context` kept to a sentence or two. Pong refuses `approves: plan` without one.

## Resources

Attach what the human needs to see instead of pasting it into the question or `context`. Pass `resources` to `send_ping` or `send_note`, each with a `title` of two to four words, such as "Migration plan" or "Failing test output", a `kind`, and either `file` (a location; Pong copies it now) or `content` (inline text). The kinds:

- `image`: a screenshot or a picture.
- `document`: a plan, a write-up or anything longer than a short paragraph.
- `sheet`: a table, from a `.csv`, `.tsv` or `.xlsx` file, or comma-separated `content`.
- `slides`: a deck, from a `.pdf` or `.pptx` file.
- `link`: one URL in `content`, such as a source you read, a prototype, a preview deploy, a pull request or a ticket. Send one link Resource for each URL. Every link goes here: Tags take none.
- `change`: a diff.
- `error`, `output`: an error, or command output.
- `file`, `text`: any other file, or short text.

## Tags

A Tag is short text with an icon. Pass `tags` to `send_ping` or `send_note`, each with `text` (at most 30 characters), `icon` and, when needed, `colour`; the tool lists the icons and colours.

- Put a kind Tag first on most Pings, saying what you are asking for:
  - Design question (`pencil`)
  - Prototype feedback (`eye`)
  - Approve command (`terminal`)
  - Approve plan (`doc.text`)
  - Review change (`chevron.left.forwardslash.chevron.right`)
  - Bug found (`exclamationmark.shield`)
- A finished Ping takes no kind Tag, and no Tag such as "Finished work" or "Done": only `finished: true` says the work is finished. Pong draws the Finished mark on it and drops any Tag that says it again. Tags for where the work landed, such as a branch or an environment, are still welcome.
- After the kind Tag, add Tags for where the change lands or what it concerns: an environment such as prod, a branch, another service, a remote host, or the ticket the work is filed under.
- Give a Tag a colour only when it is worth spotting at a glance, such as prod.
- A Tag is only read, never clicked. To give your human somewhere to go, such as the pull request a branch Tag names, attach a link Resource.
- Your human may ask for particular Tags or links in their Harness's own instructions. Follow them.

## Notes

When nothing is being asked, send a Note with `send_note`: news the human should see, such as a long job finishing or a decision you made on your own. Attach what makes the news one click away, such as a link to the preview you deployed. A Note needs no Pong and never holds you up. Do not send a Ping that only informs.

## Thoughts

As you work, send a Thought with `send_thought` each time you find, decide or change something: one plain sentence, at most 140 characters, that your human understands without the code open. It shows only in the Session's Thread, between your Pings, so that a question arrives with the context that makes it quick to answer. It never asks anything, never notifies and returns at once. Give it a `summary` when where the Session stands has changed.

A Thought states a fact. It is never what you are running, never output, and never a plan you have not started. Most turns need one to three.

- Good: "The duplicate Pongs come from Inbox dropping the Session id on retry."
- Bad: "Running the tests in internal/engine." That is what you are running.
- Bad: "Next I'll look at how Inbox retries." That is a plan not yet started.

Anything that needs answers, Resources or Tags is a Note or a Ping, not a Thought. If Pong cannot take a Thought, carry on: nothing was being asked.

## A finished Ping

When you finish a piece of work, send a Ping with `finished: true`. Your human reads this Ping, not your closing message or your Harness's own recap, so everything they need is in it:

- in the question, what was done;
- in `context`, what changed and why it works, and the cause if you found one: "Zoom now scales a plain box around each window's content, since SwiftUI ignored the view's bounds", not "Zoom is fixed";
- in `not_verified`, always, what you did not check: tests not run, a fix not tried in the app, a case you assumed. Write "nothing" only if you checked it all, and never call something checked that you did not try;
- the test output as an `output` Resource when you ran tests. Pong attaches the uncommitted diff itself;
- what you suggest next, as up to four answers, the recommended one first.

If the work is not committed, committing it is usually the recommended answer. A question you would close the work with, such as whether to commit, is one of these answers, not a Ping of its own: your human then answers it on the finished Ping.

Pong adds "Nothing more" as the last answer itself; do not add it.

### Work for a new Session

When your human asks in Pong for work to go on in a new Session, offer that work as an answer on your finished Ping instead of starting the Session yourself. Your Harness refuses a Session you start on a Pong's say-so, because a Pong is not a message your human typed. Its label names the work, such as "Add the export button", and its `leads_to` says what the new Session should do. Your human sends that answer with New Session with this Pong, and Pong starts the Session in this folder, with the Pong and your Ping as its first prompt. Only a finished Ping offers this: a Pong to a blocking question always comes back to you.

## The summary

Give every Ping and Note a `summary`: one or two lines on where this Session stands now, what it is doing and what is left, such as "Moving the login form to the new token API; the tests are next." Your human sees the latest one when they look at the Session's Thread, beside how long it has run and how full its context is, so keep it current rather than repeating the question.

## Waiting

`send_ping` returns the Pong if it comes quickly, otherwise a `ping_id`. After sending your questions, end your turn as you usually would: Pong brings every Pong back together. Call `wait_for_pong` with the `ping_id` only when you cannot go on without the answer; each call returns after a short wait. If you find the answer yourself meanwhile, call `withdraw_ping`.
