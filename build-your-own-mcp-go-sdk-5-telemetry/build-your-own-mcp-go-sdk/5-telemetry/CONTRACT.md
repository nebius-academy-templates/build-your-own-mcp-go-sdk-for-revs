# Log Triage: telemetry contract

The server implements the [complete HTTP-stage contract](../4-agents-and-clients/CONTRACT.md), with the telemetry additions below. Version is `0.5.0`. Tools, resources, prompts, schemas, instructions, error texts, authentication and scan ownership stay unchanged.

## Links

- [MCP semantic conventions](https://github.com/open-telemetry/semantic-conventions-genai/blob/main/docs/gen-ai/mcp.md)
- [OpenTelemetry Go](https://opentelemetry.io/docs/languages/go/)

## Export and lifetime

- Use OpenTelemetry Go v1.46.0 and OTLP/HTTP protobuf. Keep go-sdk v1.7.0.
- Enable export only when `OTEL_EXPORTER_OTLP_ENDPOINT` or `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT` is nonempty. With neither, there is no exporter or telemetry network traffic.
- Read endpoint and header settings through the OTLP exporter. The signal-specific endpoint takes precedence and is a complete URL. The general endpoint is a base URL. `OTEL_EXPORTER_OTLP_HEADERS` supplies authentication.
- Resource `service.name` defaults to `log-triage`, overridden by nonempty `OTEL_SERVICE_NAME`. `service.version` is `0.5.0`.
- Use the SDK's default parent-based sampler. A sampled parent records its child spans. An unsampled parent propagates context without exporting spans. A request without a valid parent starts a sampled root.
- On stdio EOF, SIGINT or SIGTERM, and on HTTP SIGINT or SIGTERM, finish active work and flush ended sampled spans with a fresh shutdown context bounded to five seconds. A reachable receiver gets the final batch. Delivery to an unavailable receiver cannot be guaranteed.
- Diagnostics go to stderr. Telemetry never writes to the server's stdout. Report export failures without printing endpoints, headers or credentials.

## Request spans

Create one SERVER span per dispatched MCP request, and none for notifications. A malformed frame rejected before middleware may have no span.

| Request | Span name | Attributes |
|---|---|---|
| `tools/call` | `tools/call <tool>` | `mcp.method.name`, `gen_ai.tool.name`, `gen_ai.operation.name=execute_tool` |
| `prompts/get` | `prompts/get <prompt>` | `mcp.method.name`, `gen_ai.prompt.name` |
| `resources/read` | `resources/read` | `mcp.method.name`, and `mcp.resource.uri` once the read succeeds |
| Other requests | Method name | `mcp.method.name` |

Use registered names only. Unknown tool or prompt names use the method-only span name and omit the target attribute, so arbitrary input does not become telemetry.

Successful spans have no `error.type`. A tool result with `isError: true` has status ERROR and `error.type=tool_error`. JSON-RPC failures use their numeric code as the error type, except request errors `-32700`, `-32600`, `-32601`, `-32602` and `-32002`, which are not server failures. A timeout or cancellation uses a fixed low-cardinality error type. Status descriptions contain no argument or result text.

At debug level with telemetry enabled, the existing per-call `tool=` log line also includes `trace_id=` followed by the trace's 32 lowercase hexadecimal digits. Without telemetry, the existing logging behavior stays unchanged.

## Context and caller

Extract string `traceparent` and `tracestate` from request `params._meta` before opening the SERVER span. A valid parent supplies its trace ID and parent span ID. Missing or invalid parent context starts a new trace. Apply this over stdio and HTTP, without reading HTTP trace headers. Preserve the authenticated caller in the context. A request without `params`, or without `_meta`, is answered as before and starts a new trace.

Each request has its own span. Reusing a `scan_id` never reuses another request's trace. A request rejected by HTTP authentication has no MCP handler span.

## Read spans

Each opened decompressed log reader has an INTERNAL `logtriage.read` child of its request span. End the span when that reader closes. Detection and full-content reads can produce separate spans for the same file. Process one file at a time, preserving the server's memory behavior. File enumeration alone, a refused path, a stored summary resource and an empty folder need no read span. Evidence validation during `save_finding` counts as reading logs.

| Attribute | Meaning |
|---|---|
| `logtriage.files` | `1` for this reader. Summing it measures file-read operations, not distinct paths |
| `logtriage.bytes` | Bytes returned by decompressed readers, before decoding or line trimming, including read-ahead, line terminators and repeated reads for detection |
| `logtriage.lines` | Newline-delimited records in the bytes `logtriage.bytes` counts for this reader, read-ahead included, counting a final nonempty unterminated record and repeated reads. A line the read stopped inside counts as one record. Not the number of lines the parser took from them |

These are work counters, not the number of lines matching a query. Detection reads count too. Counts stop where a read fails or a limit stops it. File paths and line content never become span attributes. Sum counts and durations across the call's read spans. Duration covers reading through closing the decompressed reader, including decompression and buffering. It is not a disk-only benchmark.

Reusing a stored scan reads only the files needed for the requested group or timeline.

## Privacy

Export only the required protocol identifiers, caller identity, service metadata, counts and fixed error classifications. Over HTTP, the caller identity is `enduser.id`, the authenticated principal name. The URI of a resource the server served is allowed metadata. A URI the server refused stays out of spans, whatever its shape. Arbitrary tool and prompt argument values, result bodies and raw error messages stay out of spans, events and status descriptions.

Never export a configured bearer token, an Authorization value, OTLP authentication headers or a LangSmith key. Unexpected failures keep the existing masked MCP result. An optional exception event must use sanitized fixed descriptions, not raw input-bearing errors. Diagnostics remain in the existing server log, which needs its own access controls.

## Local trace viewer

The supplied `checks/cmd/tracetail` command receives OTLP/HTTP protobuf locally and prints trees with trace IDs, span IDs, parents, durations, statuses and attributes. It buffers across batches, because children can arrive before parents. External parents are shown as absent, never invented. Its listener binds to loopback. This viewer belongs to the check materials, not the server.
