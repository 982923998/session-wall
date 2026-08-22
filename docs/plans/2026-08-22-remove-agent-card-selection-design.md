# Remove Agent Card Selection Design

## Goal

Remove the Agent role selection and move controls from each session card while preserving the existing Agent rows, persisted assignments, project settings, Codex thread opening, and local card deletion.

## Design

The session card overflow menu will contain only the destructive local delete action. `SessionCard` and `AgentRow` will no longer accept Agent lists, selected Agent IDs, or move callbacks. The board-level assignment model remains unchanged so existing row placement and project configuration continue to work.

## Verification

Run the focused session-board model tests, build the production web image, deploy it to the existing local container, and verify in the browser that the card menu has no Agent choices and still supports delete with undo.
