---
name: principle-test-behavior-not-implementation
description: Apply whenever writing, changing, reviewing, or keeping a test. Exercise a public behavior and assert a literal observable result. Delete tests that only pin implementation details, constants, prompts, calls, or fixtures.
disable-model-invocation: true
---

# Test behavior, not implementation

A useful test calls the code the way its users do and asserts a literal result or observable effect. A test that only asserts which internal calls occurred, repeats a constant or prompt, or checks a fixture it just built is a change detector, not a defect detector.

Before keeping any test, ask:

> Would this test still pass if the subject did no useful work or every imported dependency returned its zero value?

If yes, rewrite it to observe behavior or delete it.

## Delete or rewrite these shapes

- **Weak assertion:** only checks defined, truthy, non-empty, non-panicking, type, or greater than zero.
- **Call or absence only:** only checks called/not called, nil, empty, zero length, or not equal to one wrong value.
- **Self-referential expectation:** derives the expected value from the subject or helper implementing the same rule.
- **Constant or prompt pin:** restates a limit, default, table row, configuration string, prompt phrase, error wording, stage name, or other implementation text.
- **Fixture asserts fixture:** reads back data prepared by the test without proving that the subject transformed it correctly.
- **Internal routing detector:** asserts a concrete helper, adapter, branch, counter, or call sequence instead of the externally visible result.

## Required test shape

1. Call the real subject in the test body through its public or package-level contract.
2. Use one concrete input representative of a user workflow.
3. Assert a literal output, persisted state, emitted artifact, protocol response, or other observable effect.
4. For an absence rule, pair it with an input where the behavior must be present.
5. Prefer the real seam over mocks. If a fake is required, assert the semantic payload or final state, not merely that the fake was called.
6. If no observable defect can make the assertion fail, delete the test.

Relations across independently maintained tables and compile-time type checks are allowed when they prove a real consistency contract.

Adapted from Cursor's `principle-test-behavior-not-implementation`: <https://github.com/cursor/plugins/blob/main/pstack/skills/principle-test-behavior-not-implementation/SKILL.md>.
