## Context

The problem in one sentence: who suffers, what they see today, what they should see.
If it is a bug, how to reproduce it. If it is an improvement, what made it worth doing now.

## What changes

The solution, and why this shape rather than another. Cite `file:line` for anything central.

## What does NOT change

The boundary, stated. Without it a fix reads as a refactor.

## How to validate

| Level | What it covers | Result |
|---|---|---|
| N1 | `{{CMD_TEST}}` + `{{CMD_TYPECHECK}}` + `{{CMD_BUILD}}` | |
| N2 | The production artifact opens — not the development one | |
| N3 | End to end with real data, at `{{LOCAL_URL}}` | |
| N4 | Hostile environment, where it applies: another engine, another viewport, a host page | |

**Not covered:** what was left out, and why.

## Behaviour change

Yes/No. If yes: what the person using this now sees differently.

## Risk and rollback

The worst path is the one that LOOKS like success. Name the SHA to `git revert` to.
If it touched `{{RED_ZONE}}` or `{{CRITICAL_ASSET}}`, say so here.

## Merge order

If it stacks on another PR, say which and why the order matters.

<!--
═══════════════════════════════════════════════════════════════════════════════
WRITING RULES — delete this block before opening the PR

Write every PR in the same language as the rest of the repository's history. One PR in
another language is a seam.

No internal-tool attribution. Not "the reviewer agent found", not "the second opinion said".
The team reads this as one developer's work: a finding enters by what it is and how it was
proven, and the proof is what carries authority, not who produced it.

Few emotes. At most where they mark real severity inside a table. A PR body is not a chat.

Result, not intention. "Ran N2" is worth nothing; "N2: mounts, console clean" is.
Paste the number.

Declare what was NOT covered. A PR listing only what passed is read as full coverage.
A written limit is worth more than a hidden one.

Title: `type(scope): objective description`, and `[n/N]` when the PR is one of a queue.

───────────────────────────────────────────────────────────────────────────────
THIS PROJECT'S TRAPS

A green suite proves nothing about an environment the suite does not run in. If the incident
happened, write it here with its date — a rule with no story attached is a rule people skim.

<symbol> — <what it cost> → <what to check before pasting the result>

A test that disappears needs accounting. If the suite went from N to M, say where the missing
ones went, file by file. A deleted test with no written reason is a regression in disguise.
═══════════════════════════════════════════════════════════════════════════════
-->
