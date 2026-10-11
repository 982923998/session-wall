# Official desktop message execution

Session Wall loads an unloaded task through `codex://threads/<id>`, verifies
that the writer belongs to the existing desktop application, then calls the
official `codex queue` command. The public send handler does not create a web
executor or call `thread/resume` / `turn/start` on an independent app-server.
The task link can change the desktop's selected conversation.

The queue acknowledgement is shown as queued. Native transcript records provide
execution status and replies. Model and reasoning settings follow the native
task; explicit mismatches are rejected. The stop button opens the task in the
client, where the user stops it. Direct web interruption is not implemented.

If desktop ownership cannot be verified, input is not queued. There is no fallback
to a web executor. Catalog and transcript readers remain read-only and may use
separate protocol clients.

## Version and recovery

- Previous source: `40000a1073dcf5bd41df85267bdff5e3ca4f0602`.
- Previous Git tag: `session-wall/pre-official-app-20261011` (pushed).
- Implementation branch: `codex/official-app-send`.
- Runtime backup: `/Users/chenmayao/Library/Application Support/SessionWall/backup-official-app.QrJiRV`.
- The backup contains the prior executable, backend/frontend LaunchAgent plists,
  project selection configuration and a verified `source.bundle`.

For recovery, restore the tagged source on a clean checkout and the backed-up
backend executable. The frontend uses the checkout directly. Restart the Card
backend only after confirming it is not executing a task; no Codex history or
research files need to be restored.

## Validation on 2026-10-11

Local tests verify that both loaded and unloaded tasks use only the task link
and queue, and do not start an independent executor.

Two user-authorized messages were sent through the production HTTP endpoint to
the existing `Card 控制验证` task (`01a08089-fb23-7d42-bfe5-89a968d8a56a`):

- Initially not loaded: turn `01a128f5-c4f8-78e3-96f6-e92c57edac26`, reply
  `CARD_UNLOADED_OK`, completed in 5211 ms after turn start.
- Already open in the App: turn `01a128f6-6139-7712-ad86-cf932ddc6546`, reply
  `CARD_APP_OK`, completed in 1953 ms after turn start.

Both turns had only a user message and an assistant reply. The same official
desktop backend PID 95507 held the test task writer. The Card backend had no
execution child. Busy-task interruption and third-party-owned tasks were not
tested; model response latency is not guaranteed by this routing change.
