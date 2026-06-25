# Changelog

All notable changes to this project are documented in this file.

## commits `4239fad`–`fc455a6`

### Changed
- Extended the `Makefile` with new targets: `fmt` (`gofmt -w .`), `build`
  (`go build ./...`), `lint` (`go vet ./...`), `mock` (regenerates mocks via
  `mockgen`), and a `.PHONY` declaration for all targets. Added a Makefile
  targets reference table to the README and updated the testing section to use
  `make test` instead of `go test ./...`. (`fc455a6`)

### Performance

- In `StartConversation`, title generation and the first assistant reply are now
  produced concurrently (using goroutines + errgroup), cutting end-to-end latency
  for new conversations. Tool startup loading inside the assistant is also
  parallelised. Additionally, OpenTelemetry duration histogram bucket boundaries
  were tuned to better capture request latencies. (`f50a601`)

### Fixed

- The CLI's HTTP client is now initialised with an explicit request timeout,
  preventing it from hanging indefinitely if the server is slow or unreachable.
  (`1a2a1fc`)

- The system prompt used for title generation was being silently overwritten by
  the first user message, causing the assistant to answer the question instead
  of summarising it. The prompt is now preserved correctly, ensuring conversation
  titles reflect the topic rather than the reply. (`7d79a3a`)

### Added

- The server now exposes an internal `pprof` endpoint (at `/debug/pprof`) for
  CPU and memory profiling. It also handles OS signals (SIGINT/SIGTERM) to shut
  down gracefully, allowing in-flight requests to complete before the process
  exits. (`3e211a7`)

- Instrumented the HTTP server with OpenTelemetry: a Prometheus-backed metrics
  middleware now tracks request counts, response status codes, and request
  durations; a tracing middleware records spans for each incoming request.
  An `otelx` package was introduced to initialise and configure the
  OpenTelemetry provider and Prometheus exporter. (`637bdec`)

- New `itinerary` tool introduced alongside the tools-package refactor, giving
  the assistant the ability to help users with trip itinerary queries. (`ab6fef8`)

- Connected the `get_weather` tool to WeatherAPI.com, replacing the stub
  response with real data including temperature, wind speed, and conditions.
  Extended the tool to also support forecast queries, giving the assistant
  the ability to answer questions about upcoming weather. (`50d15d6`)

### Changed

- Extracted all assistant tools (weather, calendar, datetime) into a dedicated
  `internal/chat/tools` package, each implementing a common `Tool` interface.
  A `Registry` loads and registers tools at startup — concurrently where possible
  — keeping `assistant.go` free of per-tool logic. (`ab6fef8`)

- Replaced the `sync.RWMutex`-guarded holiday cache in the calendar tool with
  `atomic.Pointer`, simplifying the locking logic and reducing contention on
  concurrent reads. (`623aa4e`)

- Extracted the `get_holidays` tool-call handling out of the monolithic
  `assistant.go` into its own `calendar.go` file, making the assistant logic
  easier to navigate and setting the stage for further tool extractions.
  (`425dae8`)

### Tests

- Added integration tests for `StartConversation` and `ContinueConversation`
  that spin up a real MongoDB instance, verifying end-to-end conversation
  creation, title population, and assistant reply persistence. (`75ce97c`)

- Added unit tests for the `Assistant`, covering the `Reply` and `Title` methods.
  Introduced a `mockgen`-generated mock for the OpenAI completions API, allowing
  tests to run without hitting the network. (`9539455`)

- Added unit tests for the `chat.Server`, covering `StartConversation`,
  `ContinueConversation`, and `ListConversations`. A `mockgen`-generated mock
  for the `Assistant` interface decouples these tests from the OpenAI API.
  (`079ea95`)

### Initial release

- Bootstrapped the project: Twirp/protobuf API with `StartConversation`,
  `ContinueConversation`, and `ListConversations` endpoints; MongoDB-backed
  conversation storage; OpenAI-powered assistant with datetime and holidays
  tools (weather stubbed); CLI client; HTTP middleware for logging and panic
  recovery. (`4239fad`)
