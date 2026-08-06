# Proposal

## Problem
Runtime conformance is marked applied, but the current checker observes only
release-journey command boundaries, models state changes for only part of the
operation registry, and has no generated sequence or byte fixture contract.
The release documents therefore claim broader conformance than the gate proves.

## Outcome
Every executable operation is judged by an independent test-local lifecycle,
actor, revision, and state-change model. Missing result envelopes, uncovered
operations or journeys, illegal generated sequences, and stale conformance
fixtures fail the gate with a named failure class.

## Scope
The runtime conformance collector, checker, generated sequences, committed
JSONL fixtures, conformance limits, release gate limits, and adoption records.

## Non-goals
No production tracer, exported tracing API, exhaustive state-space model,
performance threshold, persistence model, or change to command behavior.

## Affected capabilities
release-assurance
