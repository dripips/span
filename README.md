<div align="center">

# Span

**Self-hosted time tracking that ends in an invoice**

A week is a grid, not a feed. Start a timer in one click, see where the week went, and turn the month into invoice lines.

[![Go](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Docker](https://img.shields.io/badge/Docker-Ready-2496ED?logo=docker&logoColor=white)](Dockerfile)

[Русский](README.ru.md) · [Deutsch](README.de.md)

</div>

---

## Why

Between a notepad with hours in it and a corporate time-and-attendance system there is nothing built for one person. Cloud trackers want a subscription and an account per head; corporate ones want a rollout.

Span does one thing end to end: from pressing **start** to a list of lines you can paste into an invoice. It knows about the rate, the rounding rule, and the fact that some of the work is not billable.

## Features

- **Week grid** — projects down, days across, totals on both edges. An empty Thursday is as visible as a busy Tuesday
- **One running timer** — starting a new one stops the previous; parallel timers invent hours that never happened
- **Rounding per client** — by the minute, or up to 5 / 10 / 15 / 30 minutes, as agreed in the contract
- **Rate on the client, override on the project** — fixes under an old contract can cost less than new work
- **Billable and not** — internal calls and your own mistakes stay in the report and out of the invoice
- **Invoice lines** — one row per project: description, hours, rate, amount. Copy them straight into [GoBill](https://github.com/dripips/gobill)
- **CSV export** — the same period, one row per entry
- **Three languages** — English, Russian, German
- **Light and dark themes** — the running timer is marked by shape and label, never by colour alone
- **Single binary** — Go and SQLite, no external services
- **Demo data** — one command fills two weeks of a plausible freelance month

## Quick start

```bash
git clone https://github.com/dripips/span.git
cd span
go build -o span .
./span
```

Open [http://localhost:8080](http://localhost:8080) — the owner account is created on first launch from `-email` / `-password` (defaults `you@example.com` / `span`).

### Docker

```bash
docker compose up -d
```

### Try it with demo data

```bash
go run ./cmd/seed -db ./span.db
```

Three clients, six projects, two weeks of entries and one timer already running.

## How it works

A **client** carries the rate, the currency and the rounding rule. A **project** belongs to a client and may override the rate. An **entry** is a span of time on a project, billable by default.

Duration is computed at read time, never stored: `ceil(minutes / step) * step`, where the step comes from the client. Change the rounding rule and last month recalculates — which is the point, because the rule is part of the agreement, not of the record.

The report groups entries by client and project, and `InvoiceLines` folds the billable ones into one row per project. Unbillable time is visible in the report and absent from the lines by construction.

## Configuration

| Flag | Environment | Default | Meaning |
|---|---|---|---|
| `-addr` | `SPAN_ADDR` | `:8080` | listen address |
| `-db` | `SPAN_DB` | `span.db` | SQLite file |
| `-email` | `SPAN_EMAIL` | `you@example.com` | owner account, first launch only |
| `-password` | `SPAN_PASSWORD` | `span` | owner password, first launch only |

The session secret is generated on first launch and kept in the database, so sessions survive a restart.

## Stack

Go 1.26 · [chi](https://github.com/go-chi/chi) · [modernc.org/sqlite](https://gitlab.com/cznic/sqlite) (pure Go, no cgo) · `html/template` · hand-written CSS, no front-end framework.

The design system is written down in [DESIGN.md](DESIGN.md), the product context in [PRODUCT.md](PRODUCT.md).

## Screenshots

| Week | Report |
|---|---|
| ![Week grid](docs/week.png) | ![Report](docs/report.png) |

## License

MIT — see [LICENSE](LICENSE).
