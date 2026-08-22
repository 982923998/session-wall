# Title Role Routing and Folding Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Route sessions by title prefix and add individual plus whole-section folding for role and unassigned sessions.

**Architecture:** Extend the pure board model to return Agent rows and an unassigned collection. Persist fold flags in the existing normalized local state, then render compact/expanded variants in the Session Wall component without changing the daemon API.

**Tech Stack:** TypeScript, React, Next.js, Vitest, Docker

**Spec:** `docs/plans/2026-08-22-title-role-folding-design.md`

## Global Constraints

- The title prefix before the first `-` is the only role routing signal.
- Unknown or missing role prefixes must appear in the bottom unassigned list.
- Existing hidden-session state must remain compatible.
- Deleting a board item must never delete the native Codex thread.
- Do not add dependencies.

---

### Task 1: Title-based board model

**Files:**
- Modify: `apps/web/features/session-board/model.ts`
- Modify: `apps/web/features/session-board/model.test.ts`

**Interfaces:**
- Produces: `ProjectBoard.unassignedThreads: CodexThread[]`.
- Produces: normalized `BoardState.collapsedThreads` and `BoardState.collapsedRows` maps.

- [ ] **Step 1: Write failing tests**

Add tests proving `method-...` and `methods-...` route to `Methods Agent`, recognized prefixes route to their rows, and missing or unknown prefixes route to `unassignedThreads`.

- [ ] **Step 2: Run the focused tests and confirm failure**

Run: `pnpm dlx vitest@4.1.0 run apps/web/features/session-board/model.test.ts`

- [ ] **Step 3: Implement prefix parsing and unassigned output**

Normalize the title prefix and Agent aliases, remove the first-row fallback, and partition visible project sessions between row cards and `unassignedThreads`.

- [ ] **Step 4: Implement fold-state normalization**

Add safe empty defaults for existing local state and preserve the maps through project updates.

- [ ] **Step 5: Run the focused tests**

Expected: all tests pass.

### Task 2: Folding interface

**Files:**
- Modify: `apps/web/features/session-board/session-card-wall.tsx`

**Interfaces:**
- Consumes: `ProjectBoard.unassignedThreads`, `BoardState.collapsedThreads`, and `BoardState.collapsedRows`.
- Produces: compact/expanded session cards, collapsible Agent rows, compact/expanded unassigned list items, and a collapsible unassigned section.

- [ ] **Step 1: Add persisted toggle handlers**

Create project-scoped keys for Agent rows and the unassigned section, and toggle thread/row maps through the existing `persist` function.

- [ ] **Step 2: Add card and Agent-row folding**

Render each role card at 208px when expanded and 80px when folded. Render a single full-width Agent header when its row is folded.

- [ ] **Step 3: Add the unassigned list**

Render it below all Agent rows, one session per line, with individual folding, opening, and local deletion. Add a whole-section fold control.

- [ ] **Step 4: Update explanatory copy and counts**

Explain title-prefix routing in project settings and count both role cards and unassigned sessions.

### Task 3: Verify and deploy

**Files:**
- Verify: `apps/web/features/session-board/model.test.ts`
- Verify: `apps/web/features/session-board/session-card-wall.tsx`

**Interfaces:**
- Produces: deployed local image `multica-session-card-wall:web` on `127.0.0.1:3000`.

- [ ] **Step 1: Run focused tests and diff checks**

Run the model tests and `git diff --check`.

- [ ] **Step 2: Build the production image**

Run: `docker build -f Dockerfile.web -t multica-session-card-wall:web .`

- [ ] **Step 3: Replace the local frontend container**

Reuse port `127.0.0.1:3000` and restart policy `unless-stopped`.

- [ ] **Step 4: Verify visible behavior**

Confirm prefix routing, the bottom unassigned list, individual folding, whole-row folding, and delete controls in the local browser.

- [ ] **Step 5: Commit**

Commit the component, model, tests, design, and implementation plan with a concise feature message.
