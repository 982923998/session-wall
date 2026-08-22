# Title Role Routing and Folding Design

## Goal

Route Codex sessions to Agent rows from the role prefix in the session title, separate sessions without a recognized role into a bottom list, and support both per-session and whole-section folding.

## Role routing

The text before the first `-` is the only role signal. Matching is case-insensitive and ignores surrounding whitespace. Agent IDs and names are normalized, the `Agent` suffix is ignored, and a simple plural alias allows `method-...` and `methods-...` to match `Methods Agent`. A title without `-`, an empty prefix, or an unknown prefix is unassigned. Persisted legacy assignments remain readable but no longer override title routing.

## Board structure

Recognized sessions remain cards in Agent rows. Each card can fold from its 208px form to an 80px compact card. Each Agent row can independently fold to a single header strip.

Unassigned sessions appear after all Agent rows as a list, one session per row. Each list item can fold to a compact title-only row, and the entire unassigned section can fold to its header.

## Persistence

Per-session and per-row folded state is stored in the existing local board state. Normalization supplies empty defaults for existing users, so project names, hidden sessions, and previous settings remain intact.

## Verification

Use focused model tests for title routing and state migration, a production Next.js build for type safety, and browser checks for role routing, individual folding, whole-row folding, unassigned rendering, and unchanged delete controls.
