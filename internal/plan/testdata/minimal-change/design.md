# Design

## Boundaries
accounts/Requirement: Stable locking

## Interfaces
One local update transaction.

## Invariants
Only one update owns the lock.

## Failure behavior
Lock failure leaves account bytes unchanged.

## Integration
Existing account update owner.

## Alternatives
No distributed lock until multiple processes need it.

## Owner
internal/accounts
