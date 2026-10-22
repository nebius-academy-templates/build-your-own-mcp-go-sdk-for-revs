# Telemetry completion record

The [contract](CONTRACT.md) defines the observable behavior. Record commands and results, without tokens or keys.

## Automated checks

- [ ] T1: export is opt-in, OTLP service metadata is correct, and shutdown flushes without polluting stdout.
- [ ] T2: request spans have the required names, kinds and error classifications, with matching debug trace IDs. Metadata parents work over both transports, unsampled parents are respected, scan reuse does not merge traces, and a request without `params` is still answered.
- [ ] T3: read counters measure actual decompressed work, and scan reuse reduces reads.
- [ ] T4: exports contain no input/result payloads, refused resource URIs or tokens, caller identity is correct, and masked errors remain masked.
- [ ] The server's own tests pass.

## Manual evidence

- [ ] A trace is visible in LangSmith, and its counters in the local trace viewer.
- [ ] The investigation uses MCP tools and saves a finding with file/line evidence.

## Investigation trace notes

| Question | Observation |
|---|---|
| Q1: How many tool calls did the investigation make, and which one took longest? | |
| Q2: For that longest call: its file-read operations, decompressed bytes, and the share of its time in read spans? | |

## Cleanup

- [ ] Temporary host registration removed, or corrected to the intended project.
- [ ] Servers and local receiver stopped. OTLP and token variables cleared.
- [ ] No real credentials in tracked files or the submission.
- [ ] The LangSmith key deleted from the credential store and in LangSmith, once no longer needed.

## Wrap-up

- [ ] Time spent on this practice, in minutes:
