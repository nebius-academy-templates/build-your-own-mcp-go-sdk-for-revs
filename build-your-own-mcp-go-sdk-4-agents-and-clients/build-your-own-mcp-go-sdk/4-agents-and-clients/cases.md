# Cases for practice 4

**Related**:

> [README](README.md): Task 5 asks these through your agent host
> [hosts](hosts.md): How to connect the host
> [CHECKLIST](CHECKLIST.md): Where you grade your findings

Four questions about the logs in `s3_logs/`. Ask each one through an agent host connected to your server over HTTP, framed by the `triage` prompt, as task 5 of the README shows. Save one finding per question and commit `reports/`. The first two are required. The other two are for when you have time.

## Links
**Internal**:

- `4-agents-and-clients/README.md`
- `4-agents-and-clients/hosts.md`

## Required
### Invoice errors
> Customers got errors on the invoices page late in the morning of 2026-09-09. Which instance, when, and why?

Look in: `pods`.

### A locked database
> The API logs say 'DB is locked'. Since when, how often, is it getting worse, and what does the database say?

Look in: the whole log root.

## Optional
### A CI run full of errors
> The 14:00 CI run on 2026-09-02 had hundreds of errors. Why?

Look in: the whole log root, on 2026-09-02.

### Failing certificate orders
> Certificate orders were failing at some point. When did it start, is it still happening, and do the CI results agree?

Look in: `pods`.

## A good finding
- It answers every part of the question, in the question's order.
- Every claim rests on a line the finding cites by file and line, and each cited line says what the finding says it does.
- Times are UTC, and where two sources disagree on time, the finding says which one it trusts and why.
- It says what the logs don't show, instead of guessing.
- Something that happens every day is named once as background, not as the cause.
