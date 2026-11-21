# Pong's Settings

What each setting in the Mac app does. The app keeps its own text short; the detail is here.

## Card

- **Corner** is where the card sits on the screen.
- **Move the card to the display you are working on** follows you between displays. Off, the card stays on your main display.

## Ping sound

- **Play a sound** is Off, First Ping only (the first Ping of a Thread), or First and follow-up.
- A follow-up is a Ping from a Session within five minutes of its last answer. It plays a softer sound.
- Notes make no sound.

## Voice

- **Read Pings aloud and answer by speaking** turns voice on.
- **Listen when a Ping appears** starts listening as soon as a Ping is read. Otherwise, start from the microphone on the card.
- Speech is recognised on this Mac. No audio ever leaves it.

### Model

A model understands what you say when it is more than a plain choice. Only the words you say and the Ping's question, answers and context go to the model; never audio.

- **None:** say a number, "recommended", "yes" or "no".
- **Hosted:** Pong's own model, with Pong Cloud. Nothing you send it is kept.
- **Endpoint:** any OpenAI-compatible endpoint, such as a local model. Give its URL and the model's name.
- **Anthropic API key:** your own Anthropic API key. The key is kept in the macOS Keychain, never in Pong's settings or on Pong's servers.

**Test** sends a sample Ping and says what the model understood.

## Start at login

Starts Pong when you log in. If macOS has not allowed it yet, open System Settings > General > Login Items and switch Pong on.

## Devices

- **Connected on Local:** your Devices talk directly over this network, and nothing passes through Pong's servers.
- **Connected through Pong Cloud:** your Devices reach each other from any network, sealed so Pong's server cannot read them. Sign in on one Device; the others follow without signing in.
- **Revoke** removes a Device from your set at once. It can no longer answer or start anything until it is paired again.
- To add a computer, run `pong devices pair` on it. If both show the same code, approve it here and run `pong devices confirm` there. Approve a Device asking to join only if it shows the same code.

## Harnesses

- **Installed** and **Connected** say whether Pong found the Harness and is set up in it. **Repair** sets it up again.
- **You work in** says where Pong starts a Session you start from Pong:
  - **Its app:** with no terminal window, moved into the Harness's app once its first turn ends.
  - **A terminal:** in a terminal window.

## Projects

Each Project's menu sets its name, colour and logo. **Take Sessions started on your other Devices** lets this computer run Sessions started in that Project from your phone or another computer.

## Sending a Pong

The caret beside Send offers:

- **New Session with this Pong:** starts a fresh Session in the Project, carrying your Pong as context.
- **Send to this Session:** continues where it left off. It shows how full the Session's memory is.
- **Answer in the Harness instead:** Pong lets go of the Ping, and the Harness asks you there.

## Empty lists

- A Project appears once a Session in its folder sends a Ping. Until then, pick a folder when starting a Session.
- A Thread appears once a Session starts or sends a Ping.
