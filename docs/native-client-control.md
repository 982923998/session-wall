# Native client control

Session Wall forwards message actions to the existing Codex desktop application.
The web server does not resume a second writer for send or stop. Read-only catalog
and transcript loading remain unchanged.

## Runtime

- Web/API: existing Session Wall service on loopback port 19514.
- UI control: `scripts/native-ui-chat.cjs`, loopback port 19515, managed by
  `com.chenmayao.session-wall.ui-control` on this Mac.
- The existing `Session Wall Controller.app` uses user-approved macOS
  Accessibility access. Do not rebuild it casually: a changed executable can
  require permission again.
- UI control requires a private 0600 `ui-control-token` below the user's
  `Library/Application Support/SessionWall` directory. The API reads the same
  token. Browser connections to the UI-control socket are rejected.
- No CLI replacement, shared runtime, second desktop instance, or private
  desktop-tool bridge is involved in message execution.

## Behavior

Sending checks the task identity, protects existing drafts, fills the native
composer and invokes native submission. Busy tasks use the client's queue.
Stopping targets the native active task and is reported successful only after
an interrupted turn is observed.

Receipts distinguish submitted, queued, started, completed, interrupted and
unknown. A local submission ID is replaced by the native message ID for display
deduplication. Unknown delivery is never automatically retried; the original
input is retained and the user must check the client before unlocking resend.

Router therefore uses its native tools and its own skill to create and initialize
tasks. The web route no longer injects the title-only task creation substitute.

The default model option follows the client's current settings. Explicit model
and effort selections are checked before sending, but this version does not
automatically manipulate the native model menu. Change mismatched settings in
the client first; messages are not silently sent with a different model.

## Limits and validation

The client must be running with an unlocked usable desktop and Accessibility
permission. Native task navigation may briefly change the visible task. Existing
drafts block automation rather than being overwritten. These GUI actions cannot
provide an atomic guarantee against simultaneous physical typing.

Tests cover routing, receipts, draft protection and stop confirmation using
fixtures. The live acceptance check prepares and clears a draft in the existing
test task and verifies that history did not change. It does not send a real model
request or claim a live queue/stop test passed.

Baseline: `5ca7b60be` on `main`. The abandoned, unfinished shared-runtime experiment
is preserved separately at `64f189d55` on `codex/shared-runtime`; it is not this
runtime's implementation.
