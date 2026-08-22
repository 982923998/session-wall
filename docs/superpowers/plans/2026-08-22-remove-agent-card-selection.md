# Remove Agent Card Selection Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Remove Agent selection from session cards while keeping Agent rows and card deletion intact.

**Architecture:** Simplify the component prop chain in `session-card-wall.tsx`; do not modify the persisted board model. Validate existing model behavior and the production UI.

**Tech Stack:** React, TypeScript, Next.js, Vitest, Docker

**Spec:** `docs/plans/2026-08-22-remove-agent-card-selection-design.md`

## Global Constraints

- Preserve Agent rows and existing persisted assignments.
- Keep card opening, deletion, and undo behavior unchanged.
- Do not add dependencies.

---

### Task 1: Simplify session card actions

**Files:**
- Modify: `apps/web/features/session-board/session-card-wall.tsx`
- Test: `apps/web/features/session-board/model.test.ts`

**Interfaces:**
- Consumes: Existing `SessionCard`, `AgentRow`, and `BoardState` interfaces.
- Produces: A `SessionCard` menu exposing only `onDelete` and an `AgentRow` without move-related props.

- [ ] **Step 1: Confirm current focused tests pass**

Run: `pnpm dlx vitest@4.1.0 run apps/web/features/session-board/model.test.ts`

- [ ] **Step 2: Remove Agent selection props and menu items**

Delete `agents`, `selectedAgentId`, and `onMove` from `SessionCard`; delete `allAgents` and `onMove` from `AgentRow`; remove `moveThread`; retain the delete menu item.

- [ ] **Step 3: Run focused tests**

Run: `pnpm dlx vitest@4.1.0 run apps/web/features/session-board/model.test.ts`
Expected: all tests pass.

- [ ] **Step 4: Build and deploy**

Run: `docker build -f Dockerfile.web -t multica-session-card-wall:web .`
Expected: production build succeeds.

- [ ] **Step 5: Verify the browser behavior**

Open `http://127.0.0.1:3000/session-board`, inspect a card menu, and confirm only `删除卡片` appears. Confirm delete and undo still change the card count correctly.

- [ ] **Step 6: Commit**

Stage the component and plan documents, then commit with `feat: remove agent selection from session cards`.
