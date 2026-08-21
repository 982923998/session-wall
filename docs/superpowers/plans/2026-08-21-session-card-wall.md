# Session Card Wall Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the local Multica experience with a focused project/Agent-row card wall for existing Codex conversations.

**Architecture:** The host daemon reads Codex thread summaries through app-server and exposes a loopback-only endpoint. A standalone Multica-styled page stores project, Agent-row, and card assignments locally and opens cards with `codex://threads/<id>`.

**Tech Stack:** Go, Codex app-server JSON-RPC, Next.js, React, browser local storage.

**Spec:** Current conversation requirements.

## Global Constraints

- Never call `thread/start`, `thread/fork`, `thread/delete`, or `thread/archive`.
- Do not copy full conversation histories.
- Do not expose issue states, inbox, chat, autopilot, usage, runtime, skill, or squad-management surfaces.
- One project is one board; one Agent is one row; one Codex thread is one card.

---

### Task 1: Read-only local thread catalog

- [ ] Add failing daemon tests for response decoding, local CORS, and method restrictions.
- [ ] Implement `GET /codex/threads` using only Codex `thread/list`.
- [ ] Run focused and package-level Go tests.

### Task 2: Focused local card wall

- [ ] Add failing tests for board normalization, grouping, sorting, and deep links.
- [ ] Implement the standalone Multica-styled page and local persistence.
- [ ] Make the local frontend open the card wall as its default route.
- [ ] Run focused frontend tests, typecheck, and build.

### Task 3: Local deployment

- [ ] Build and install the modified daemon.
- [ ] Build and launch the focused frontend without restoring the deleted Multica database.
- [ ] Verify real Codex threads appear and one card opens its exact desktop conversation.
