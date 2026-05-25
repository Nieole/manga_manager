# Storage IO Baseline Template

- Captured at:
- Run label:
- Library path:
- Library volume:
- Cache dir:
- Cache volume:
- Same volume:
- Storage profile under test:
- Notes:
- Windows disk active time peak:
- Windows average response time peak:
- Subjective Explorer responsiveness:
- Subjective reader page-turn responsiveness:

## Results

| Test | Files | Bytes | Duration | Throughput |
| --- | ---: | ---: | ---: | ---: |
| walk+stat |  |  |  |  |
| sequential-read-c1 |  |  |  |  |
| concurrent-read-c2 |  |  |  |  |
| concurrent-read-c4 |  |  |  |  |
| small-file-write |  |  |  |  |

## Reader Latency Under Background Reads

| Test | Probes | Reader bytes/probe | Background bytes | Duration | P50 | P95 | Max |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| reader-latency-unthrottled |  |  |  |  |  |  |  |
| reader-latency-low-impact |  |  |  |  |  |  |  |

## Decision Summary

| Item | Value |
| --- | --- |
| Best read concurrency |  |
| c2 throughput gain vs c1 |  |
| c4 throughput gain vs c1 |  |
| Low-impact reader P95 change |  |
| Recommended archive_open_concurrency |  |
| Recommended cover_concurrency |  |
| Recommended hash_concurrency |  |
| Move cache to SSD |  |

## Interpretation

- Keep or change `archive_open_concurrency`:
- Keep or change `cover_concurrency`:
- Keep or change `hash_concurrency`:
- Move cache directory:
- Low-impact reader latency improvement:
- Notes:
