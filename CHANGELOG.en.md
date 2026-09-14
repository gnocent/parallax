# Changelog

*[Français](CHANGELOG.md) · English*

No SemVer-style version numbers: Parallax is not distributed as packages,
only built and deployed. The milestones below (v1, v2, v3…) follow the
project's functional breakdown, most recent first.

## v3 — Convenience and automation

**Capacity view.** A screen comparing need and installed capacity across
the whole fleet: in rows, clusters grouped by the usual axes (project,
environment, technology, tier, usage, cluster); in columns, each component
targeted by a rule, with two sub-columns need / capa — under real data or
a scenario, for a given year, or as a multi-year projection with the
"year" axis. Exportable. Along the way, the "model year" axis (generation)
joins the view builder and the comparison, and every table with axes
merges same-valued cells vertically — a true hierarchical view. Axes are
now chosen with tags (click or drag and drop, reorderable, up to six) and
column headers sort on click, within each parent group. Displayed
columns are chosen the same way, in the desired order; by default:
servers, cores, RAM, HDD, SSD, nodes.

**Scenario summary and batch sizing.** A scenario now has a home page:
all its clusters with, for each one, the limiting rule and its figures
before (real data) and after (scenario), server arrivals and departures
and the cost of additions — clusters still in deficit stand out, with a
direct "Size" link. A shortcut opens the comparison already configured
(real data against this scenario, per cluster, servers, costs, licenses).
Batch sizing chains reverse sizing across all clusters at once: kept
generations chosen once, candidate model per tier or per cluster, a
line-by-line preview, then materialization in a single transaction.

**Automatic local backup.** A copy of the database is written to disk
every day, at the time configured from the Backup screen — but only if
something changed since the previous one, so quiet nights never pile up
redundant files. Copies older than the configured retention are deleted
automatically. A dedicated button writes one immediately, to check the
chosen folder works. Independent from the periodic S3 deposit, which
remains available alongside it.

**LDAP authentication.** Sign-in through an LDAP directory (simple bind over
LDAPS, no service account: it is the connecting person's own identity that
verifies their password). An account unknown to Parallax but recognized by
the directory is created automatically as a reader; an administrator then
adjusts its role. Local accounts remain available as a fallback — useful if
the directory is unreachable.

**Bulk server update.** A second import, alongside the one that creates
servers: this one modifies existing real servers from a file — hardware
receipt, decommissioning a batch, reassignment, model change, recording
request numbers. The file carries only the columns to change (a missing
column or an empty cell changes nothing) and a special value clears a field.
Before any write, a line-by-line preview shows exactly what will change. A
button on the server list exports directly in this format, for a round trip
through a spreadsheet.

**Change log.** Every creation, modification or deletion is tracked: who,
when, on which record, with the before and after state. A dedicated page
lets you filter by entity, user or period; each record shows its own
history. Deliberately, the log does not let you undo an action — it serves
to understand a mistake, not to roll back automatically, which could have
recreated inconsistencies.

**IP addressing.** A catalogue of VLANs, each with its address ranges and the
criteria that determine where it applies (project, environment, zone,
cluster). When a hypothesis becomes real, Parallax proposes free addresses
for all the servers involved in one move; two simultaneous hypotheses are
never offered the same address. Inconsistencies (duplicate address, out of
range, a VLAN that doesn't match the assignment) are flagged on a dedicated
page, without ever blocking you.

**Software licenses.** The number of licenses owed for a technology becomes
a calculated column in the view builder, with several possible calculation
modes (by installed nodes, by per-machine memory cap, or global) set by an
annual contract. The contract can change mode from one year to the next
without touching the fleet.

**Hardware request generation.** The text to paste into the company's
ordering tool is generated automatically for each server, from a
customizable template (`{variable}`). A dedicated screen lists the servers
to order, offers the ready-to-copy text, and lets you record the resulting
request number across a whole batch of servers in one move.

**Universal export.** Every table shown in the application — not just the
view builder's views — exports to CSV or Excel in one click, with the
filters currently active on screen.

**Fleet view as of a past date.** The view builder, saved views and scenario
comparison can all be placed as of an earlier date — useful in particular
for knowing license consumption at a given point in time.

## v2 — Simulation

**Scenario comparison.** Two hypotheses (or one hypothesis and the real
data) placed side by side, with the aggregates of your choice, exportable.

**Reverse sizing.** Starting from a target need on a cluster and a year,
Parallax compares several candidate models (number of servers needed,
acquisition cost, annual cost, capacity obtained) and materializes the
chosen option as hypothetical servers in the scenario, accounting for what
is already installed and kept.

**Sizing constraints.** Rules that complement the pure capacity calculation
— a minimum number of servers, a multiple, a spread across availability
zones — defined per cluster, per year and per scenario.

**Layered scenarios.** The core of the simulation: a hypothesis can move,
add or remove servers, and override calculation parameters, without ever
modifying the real data. It can be compared, promoted into the real data, or
abandoned at any time, with no re-entry. Every screen (views, need/supply)
can be viewed under a scenario.

## v1 — A reliable, browsable reference

**View builder.** The equivalent of pivot tables: free grouping axes,
multiple filters, aggregated columns of your choice, saved and shared views,
Excel export.

**Formula engine.** Capacity calculation rules, written as short expressions
and parameterized by variables resolved through a hierarchy (cluster, tier,
technology, environment, project, global). Two evaluation levels depending
on the formula: across a whole scope, or per server then aggregated. A
per-cluster screen compares the calculated need to the installed supply and
identifies the limiting rule.

**Initial import.** Bulk recovery of existing models and servers from CSV
files, with a simulation mode that lists every anomaly before any write —
never a partial import.

**Clusters and inventory.** Clusters (grouped by project, environment,
technology, tier and usage), servers and their dated assignments to a
cluster, with full history kept at every reassignment. The state of the
fleet is browsable as of any past date, with the hardware characteristics of
that time.

**Hardware catalogue.** Server models, their revisions (immutable — any real
hardware change creates a new revision, distinct from a simple data-entry
correction) and their components (cores, RAM, disks, network, GPU), which
feed directly into the formula engine.

**Foundation.** Local accounts with roles (reader, editor, administrator),
periodic backup to S3-compatible storage, a self-contained binary with no
external service to install.
