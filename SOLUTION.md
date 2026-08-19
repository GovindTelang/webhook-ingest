# Solution

## What was broken

The webhook provider delivers events at least once, so the same `event_id` can be delivered more than once. The original ingestion flow first checked `EventExists()` and then inserted the event. This created a check-then-act race: two concurrent deliveries could both see that the event did not exist and both apply the event's side effects.

I reproduced this with a concurrent regression test. Before the fix, 20 simultaneous deliveries of the same event produced multiple rows in `events`.

I moved the idempotency decision into Postgres by adding a unique index on `event_id` and using `INSERT ... ON CONFLICT DO NOTHING`. Event insertion, the call upsert, and the durable account-stat update are now performed in one transaction:

```text
BEGIN
  |
  +--> insert event_id
  |       |
  |       +--> duplicate -> no side effects
  |       |
  |       +--> new event
  |              |
  |              +--> upsert call
  |              |
  |              +--> increment account stats
  |
  +--> COMMIT
```

The in-memory stats cache is updated only after the transaction succeeds.

Recording processing had a separate lifecycle problem. The background recording worker was using the HTTP request context, even though the work was intentionally asynchronous. That context could be cancelled when the request ended, causing the recording update to fail. The worker also discarded errors.

I gave each recording worker its own bounded context and log processing failures. I also added a `WaitGroup` so shutdown waits for accepted recording jobs instead of allowing the process to exit while they are still running.

Finally, `stats.Cache.Record()` modified a shared map without taking the cache mutex. `Get()` was already protected, but writes were not. I protected writes with the mutex and added a concurrent test covering 100 simultaneous updates.

## Why I chose Postgres for deduplication

I chose Postgres because `event_id` is durable business state and Postgres already owns the event, call, and account-stat data.

A unique constraint/index provides a concurrency-safe way to decide which delivery is the first one. Keeping the event insert and its durable side effects in the same transaction also prevents partial database state if one of the later operations fails.

Redis is available in the service, but using it as the authoritative deduplication store would introduce another consistency boundary between Redis and Postgres. Redis can still be useful for cached or derived data without becoming the source of truth for event idempotency.

## What I would change at 10,000 webhooks/second

At that volume, I would decouple fast webhook acknowledgement from database processing using a durable queue or stream:

```text
Provider
   |
   v
Load-balanced API instances
   |
   v
Durable queue / stream
   |
   +---- Worker ----+
   +---- Worker ----+----> Postgres
   +---- Worker ----+
          |
          +---------> Redis / derived cache
```

The API layer could validate and durably enqueue the event, then acknowledge the provider quickly. Workers could scale horizontally based on queue depth.

I would also evaluate batching, database indexing and partitioning based on actual data volume, backpressure, and stronger metrics/tracing. Postgres would remain the authoritative store for durable event state and idempotency.

## Regression tests added

* Concurrent duplicate webhook deliveries
* Duplicate `ProcessEvent` calls at the store layer
* Recording processing after the webhook request returns
* Concurrent updates to the in-memory stats cache

The existing API and store tests also continue to pass after the changes.
