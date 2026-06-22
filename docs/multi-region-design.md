# Multi-Region Design — Mint

## Overview

This document outlines the design considerations for deploying Mint across
multiple geographic regions. The goal is low-latency validation in each region
while maintaining global consistency for tenant and key management.

## Component Consistency Requirements

| Component | Consistency Model | Notes |
|-----------|------------------|-------|
| Postgres (tenants, keys) | Strong consistency (single writer) | Source of truth; read replicas per region |
| Redis (rate-limit buckets) | Eventual / region-local | Per-region rate limits are acceptable |
| Redis (usage counters) | Eventual | Flushed to Postgres periodically; region-local counters merged |
| Redis (L2 cache) | Eventual / region-local | Each region has its own L2; invalidation via cross-region pub/sub |
| L1 cache | Local per replica | Invalidated via Redis Streams (region-local) |

## Proposed Architecture

```
                    ┌──────────────────────┐
                    │   Postgres Primary   │
                    │     (us-east-1)      │
                    └──────────┬───────────┘
                               │ streaming replication
                 ┌─────────────┼─────────────┐
                 ▼             ▼             ▼
          ┌────────────┐ ┌────────────┐ ┌────────────┐
          │ PG Replica │ │ PG Replica │ │ PG Replica │
          │ us-east-1  │ │ eu-west-1  │ │ ap-south-1 │
          └────────────┘ └────────────┘ └────────────┘
                 │             │             │
          ┌──────┴──────┐ ┌───┴───────┐ ┌───┴───────┐
          │  Regional   │ │ Regional  │ │ Regional  │
          │  Redis      │ │ Redis     │ │ Redis     │
          │  + Mint     │ │ + Mint    │ │ + Mint    │
          └─────────────┘ └───────────┘ └───────────┘
```

### Read Path (Validate — Hot Path)

1. Each region runs its own Mint replicas + Redis instance.
2. L1 → L2 → **regional Postgres read replica** (instead of the primary).
3. Cache miss reads are served from the local read replica — no cross-region hop.
4. Rate-limiting uses the regional Redis only.

### Write Path (Admin — Cold Path)

1. Tenant/key creation and revocation route to the **Postgres primary** in the
   home region.
2. Streaming replication propagates changes to all read replicas.
3. Revocation events are published to a **cross-region Redis pub/sub channel**
   (or a message bus like AWS SNS/SQS) so every region's L1+L2 cache is
   invalidated.

### Usage Metering

1. Each region's flusher writes to its **regional Redis** usage counters.
2. A global aggregator (cron job or Lambda) periodically sums regional counters
   and writes the merged total to the primary Postgres.
3. Quota enforcement uses the regional counter (slightly stale) — acceptable
   because over-quota by a few requests is preferable to cross-region latency on
   every request.

## CAP Trade-Off Analysis

| Failure Scenario | Partition | Behavior |
|-----------------|-----------|----------|
| Regional Redis down | AP | Rate-limiting fails open; auth falls through to PG replica. L2 miss → PG read. |
| Postgres primary unreachable from a region | CP for writes, AP for reads | Admin writes fail; reads continue from local replica. |
| Cross-region pub/sub failure | AP | Revocations delayed until L1 TTL expires (5 min). |
| Regional Postgres replica lag | AP | Newly created keys may not validate for a few seconds (replication lag). Mitigation: write-through to regional L2 on key creation. |

## Migration Strategy

1. **Phase A**: Deploy regional Redis + Mint replicas pointing at a central
   Postgres (single region, but services are region-local). Validates the
   operational model.
2. **Phase B**: Add Postgres streaming replicas in each region. Switch Mint's
   read path to the local replica.
3. **Phase C**: Implement cross-region revocation propagation (SNS/SQS or Redis
   Streams with cross-region replication).
4. **Phase D**: Implement the global usage aggregator.

## Open Questions

- **Quota accuracy vs. latency**: How much over-quota leeway is acceptable
  before requiring synchronous cross-region quota checks?
- **Key issuance latency**: Is it acceptable for a key issued in eu-west-1 to
  take 1–2 seconds before it's valid in ap-south-1 (replication lag)?
- **Operational complexity**: Is the team ready to operate cross-region Postgres
  replication, or should we consider a globally distributed database (e.g.,
  CockroachDB, Spanner)?
