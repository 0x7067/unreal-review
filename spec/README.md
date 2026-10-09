# spec/

Hand-written Bend models of the Go review logic, not Go equivalence. Nat
IDs/digests stand for interned strings; byte counts stand for serialized
prompt lengths. The proofs in `PROOF.bend` hold about the models.

## What ties the models to the code

Two checks, both runnable offline under `make check` with no Bend binary:

1. **Conformance** — per-package tests pin the shared surface:
   - `review/bend_consts_test.go`: `plan.bend`/`group.bend` numeric defs equal
     the Go constants (`MaxPlanPromptBytes`, `maxBriefDiff`, `maxGroupFiles`,
     `maxGroupLines`).
   - `internal/render/bend_consts_test.go`: `github.bend cap50()` equals
     `maxInlineComments`.
   - `findings/bend_conform_test.go`: `findings.bend` Status/Anchor/Severity
     variants match the Go vocabularies in both directions (including the
     StatusNone ↔ empty-string mapping).
2. **Witness manifest** — `witnesses.txt` classifies every law in `LAWS.bend`
   as `witnessed` (a named Go test asserts the same rule against the real
   implementation) or `model-only`. `internal/specconf` fails on an
   unclassified law, an unknown law name, or a witness pointing at a test
   that no longer exists.

## Rules

- A change to logic a law describes is mirrored here in the same change.
- A new law must be classified in `witnesses.txt` in the same change. If it
  describes production behavior, name the Go test that asserts it.
- Spec files resolve from the module root (`internal/specpath`, or a
  stdlib-only walk in public packages, which cannot import `internal/`).

## Non-goal

Behavioral equivalence between a model and its Go implementation is out of
scope: differential agreement is sampled, never proven. The manifest states
exactly which laws carry a Go witness and which rest on the model proof
alone.
