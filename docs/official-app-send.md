# Official desktop message execution

Session Wall loads an unloaded task through `codex://threads/<id>`, verifies
that the writer belongs to the existing desktop application, then calls the
official queue. Text uses `codex queue`; images use `thread/queue/add` through a
short-lived protocol client because the CLI queue command rejects image attachments.
The public send handler does not create a web
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
- Verified implementation tag: `session-wall/official-app-20261011`.
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

## Images, 2026-10-24

The composer accepts file selection, drag/drop and clipboard images with removable
previews. Switching tasks clears unsent images. Text-only requests retain their
original plain-text format; image requests use multipart form data. Failed sends
retain the draft and previews. Supported formats are PNG, JPEG, WebP and GIF,
limited to 6 images, 8 MiB each and 32 MiB combined.

Images are stored privately under `~/Library/Application Support/SessionWall/uploads`.
Server-generated paths are passed as `localImage` queue inputs. A private mapping
from native client message ID to uploaded assets preserves previews after Codex
converts its persisted input to an inline image. The HTTP image route exposes only
these generated assets, not arbitrary local files. Files remain for delayed queue
consumption and chat history; uncertain submissions also retain their assets.
The protocol client only initializes and adds queue input; it never resumes a task
or starts a turn. Actual task execution remains with the desktop writer.

- Previous version tag: `session-wall/pre-images-20261024` (source `ac7183ab4`).
- Image version tag: `session-wall/images-20261024`.
- Branch: `codex/image-uploads`.
- Runtime/source backup: `/Users/chenmayao/Library/Application Support/SessionWall/backup-images.e2uvMZ`.
- Browser test: `e2e/session-board-image-uploads.spec.ts`; upload/paste/drop/removal,
  IME confirmation, plain text, failed drafts and task switching are covered with
  intercepted API requests.
- Authorized live image turn: `01a16c6b-3378-7db1-8921-07e5e32cf199`, answered
  `蓝色` for the generated blue image, without tools.
- Authorized live plain-text turn: `01a16c6b-a8a7-7602-bec5-ec8ad0e19ff8`, answered
  `CARD_TEXT_OK`, without tools.

To roll back this increment, use the pre-images tag and its backed-up executable.
Keep the uploads directory because native conversation history may reference it.
