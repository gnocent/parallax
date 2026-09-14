# Parallax — User guide

*[Français](guide-utilisateur.md) · English*

This guide explains what Parallax does and how to use it, screen by
screen. For installation and deployment, see
[`deploiement.en.md`](deploiement.en.md); for file format details,
[`import-format.en.md`](import-format.en.md).

## Table of contents

1. [Key concepts](#1-key-concepts)
2. [Signing in, roles](#2-signing-in-roles)
3. [Reference data](#3-reference-data)
4. [Hardware catalogue](#4-hardware-catalogue)
5. [Clusters and servers](#5-clusters-and-servers)
6. [Importing data](#6-importing-data)
7. [The formula engine](#7-the-formula-engine)
8. [Need, supply and gap](#8-need-supply-and-gap)
9. [Scenarios: simulating without breaking anything](#9-scenarios-simulating-without-breaking-anything)
10. [Reverse sizing](#10-reverse-sizing)
11. [Comparing two scenarios](#11-comparing-two-scenarios)
12. [The view builder](#12-the-view-builder)
13. [Hardware requests](#13-hardware-requests)
14. [Software licenses](#14-software-licenses)
15. [IP addressing](#15-ip-addressing)
16. [Change log](#16-change-log)
17. [Accounts and administration](#17-accounts-and-administration)
18. [Exporting any table](#18-exporting-any-table)

---

## 1. Key concepts

Parallax organizes its inventory around a handful of concepts; understanding
them clarifies everything else:

| Term | What it is |
|---|---|
| **Cluster** | The scope to which servers are assigned: the combination of a project, an environment, a technology, and optionally a tier and a usage. Example: `ElasticHot1`. |
| **Model** | A hardware generation corresponding to an annual order (type + year). Example: `DENSE-2025`. |
| **Revision** | A dated and **immutable** variant of a model — added disks, a changed network card. Modifying a revision already used by a server is refused; a new one must be created instead. |
| **Component** | What a revision carries: compute cores, RAM, disks (HDD/SSD kept separate), network cards, GPU. This is what feeds the capacity calculation. |
| **Server** | A physical machine, with a status (ordered, in service, decommissioned, or hypothetical within a scenario) and a dated assignment to a cluster. |
| **Scenario** | A layer of assumptions laid over reality: adding, moving or removing servers, changing a calculation value — without ever touching real data unless and until the scenario is adopted. |
| **Rule** | A formula that computes a need (in cores, in TB, in licenses…) over a given scope. |
| **Variable** | A parameter used by rules, whose value can differ by cluster, tier, technology, environment, project, or a global default. |

Central principle: **reality is a scenario like any other**, the one with
no name. Every read operation — a view, a need calculation, a
comparison — accepts being placed within reality or within a scenario,
with the same logic everywhere.

## 2. Signing in, roles

The sign-in screen asks for a username and a password. Three roles
exist:

| Role | Can do |
|---|---|
| **Reader** | View every screen, build and export views, without changing anything. |
| **Editor** | Everything a reader can do, plus data entry: reference data, catalogue, servers, scenarios, rules, imports. |
| **Administrator** | Everything an editor can do, plus account management, the hardware request template, and viewing the change log. |

On the very first startup, an administrator account is created
automatically (see [`deploiement.en.md`](deploiement.en.md)). If an LDAP
directory has been configured by the technical administration, signing in
with one's corporate username automatically creates an account with the
reader role, which an administrator then promotes to the desired role
from the accounts screen.

## 3. Reference data

Six simple lists form the application's common vocabulary, each with its
own screen ("Reference data" menu):

- **Projects**, **Environments** (production, pre-production…),
  **Technologies** (search engine, message queue…), **Tiers** (hot, warm,
  cold…), **Usages**, **Availability zones**.

Each row can be created, edited, and archived rather than deleted: an
archived project disappears from selection lists but remains visible
wherever it is already referenced — nothing is ever lost.

## 4. Hardware catalogue

"Catalogue" menu → **Models**. A model groups a type and a year; it
carries one or more revisions, each dated and made up of hardware
components (cores, RAM, disks, network, GPU — the full list of
recognized codes is in
[`import-format.en.md`](import-format.en.md#model-import)).

**Correcting a revision** (overwriting a data-entry mistake) and
**creating a revision** (an actual hardware change, adding disks for
example) are two distinct actions, deliberately kept separate on the
screen: a revision already used by a server cannot be modified without
explicitly saying so. This is what guarantees that a server viewed as of
a past date shows the hardware characteristics that were actually in
effect at the time.

Each model also carries, per technology, the number of "application
nodes" it installs — useful for technologies that count in nodes rather
than raw capacity.

## 5. Clusters and servers

"Inventory" menu.

**Clusters**: a cluster is created by choosing a project, an environment
and a technology (mandatory), and a tier and a usage (optional). Sizing
constraints (minimum, multiple, distribution across zones) can be added
there from the cluster's need/supply screen — see
[section 8](#8-need-supply-and-gap).

**Servers**: a server's record groups its attributes (name, host, serial
number, IP, VLAN, typology, application code, request number), its link
to a catalogue revision, and its assignment to a cluster — each dated,
with the full history kept at every change. The list can be filtered by
status, by zone, or by scenario.

Possible statuses:

| Status | Meaning |
|---|---|
| `COMMANDE` | Awaiting receipt. |
| `EN_SERVICE` | Installed and assigned. |
| `DECOMMISSIONNE` | Removed from the fleet — the current assignment and catalogue link are closed automatically, and the IP address is released. |
| `HYPOTHESE` | Exists only within a scenario; becomes `COMMANDE` if the scenario is adopted. |

The **Servers with no active assignment** screen lists natural candidates
for reuse, sorted by ascending lease end date.

## 6. Importing data

"Operations" menu → **Import**. Four creation imports, to be done in
this order the first time (each references by code what the previous one
created):

1. **Reference data** — a single file for the six lists from
   [section 3](#3-reference-data), with a `type` column distinguishing
   each row.
2. **Clusters**
3. **Models** (hardware catalogue)
4. **Servers**

A fifth, independent import loads the **VLAN catalogue** (see
[section 15](#15-ip-addressing)).

Each import works in two steps: **Analyze** changes nothing and shows
every anomaly detected, row by row; **Confirm** writes everything, or
nothing if the slightest error remains — never a partial import. The
precise format of each file, with examples, is detailed in
[`import-format.en.md`](import-format.en.md).

**Updating existing servers.** A separate import, "Server update",
modifies servers that already exist rather than creating them. The file
only needs to carry the columns being changed: a column absent from the
file, or a cell left blank, leaves the corresponding field untouched;
the special value `#VIDE` clears an optional field. The server targeted
by each row is found by its hostname, or by the pair request number +
record reference, or by its physical name. Before writing anything, the
screen shows a precise preview of the changes, row by row. A button on
the server list ("Export for update") directly produces a file in this
format, handy for a round trip through a spreadsheet: export, edit the
desired columns, re-import.

## 7. The formula engine

"Formula engine" menu.

**Metrics**: the quantities one wants to compute (usable disk in TB, RAM
in GB, number of licenses…), each with its unit.

**Variables**: the parameters used by the rules (daily throughput,
retention rate, fill-ratio coefficient…). A variable's value is entered
for a given year and scenario, at the most appropriate scope — a
specific cluster, or something broader (tier, technology, environment,
project), or a global default value. When calculating, Parallax always
uses the most specific value available; every value change is kept in a
browsable history, so that a correction is never lost track of.

**Rules**: a formula (an expression such as
`debit_jour * retention_jours`) that produces a need for a metric, over
the domain defined by a filter (project, environment, technology, tier,
usage, or a specific cluster). Two active rules can never overlap on the
same metric and the same scope — Parallax refuses this at save time,
naming the conflicting rule, so that there is never any silent ambiguity
about which formula applies.

A rule can be evaluated in two possible ways: **once over the
aggregates** of the entire scope, or **server by server and then
summed** — useful for caps that apply machine by machine.

## 8. Need, supply and gap

From a cluster's record: the calculated need (from the active rules),
compared against the supply actually installed, with the gap and the
limiting rule highlighted. The screen accepts a year and a scenario, and
additionally shows, under a scenario, what it changes relative to
reality (arrivals, departures).

This is also the screen where a cluster's **sizing constraints** are
managed: a minimum number of servers, a required multiple, a
distribution across availability zones — rules that complement the pure
capacity calculation, and that apply to reverse sizing (next section).

**The capacity view** ("Simulation" menu → **Capacity**) gives the same
comparison across the whole fleet at once: in rows, clusters grouped by
the chosen axes (project, environment, technology, tier, usage, cluster —
up to three); in columns, each component targeted by an active rule, with
two sub-columns, *need* and *capa*, the capa shown in red when it is below
the need. For a given year, under real data or a scenario. Per cluster and
per component, the retained need is the largest of the needs of the rules
targeting that component (two rules on the same component are competing
constraints, not additive); the clusters of a group then add up. A cluster
whose rule cannot be evaluated is flagged above the table: its capacity
counts, its need does not. The **Year** axis turns it into a multi-year
projection: enter a range (2026 to 2030, ten years at most) and each year
becomes a row, recomputed with its own variables and the supply read as of
its December 31 — without this axis, only the first year counts. When
several axes are chosen, axis cells with the same value are merged
vertically: the table reads as a hierarchy. Axes are chosen with tags
and the headers (clusters, servers, need and capa of each component)
sort on click, as in the view builder.

## 9. Scenarios: simulating without breaking anything

"Simulation" menu → **Scenarios**. A scenario has a name, a description
and a status:

| Status | Meaning |
|---|---|
| `BROUILLON` | Being prepared. |
| `ACTIF` | Under study, comparable and editable. |
| `RETENU` | Its assumptions have joined reality ("Promote" button); viewable, not editable. |
| `ABANDONNE` | Dropped without touching reality; can be resumed. |

Within an active scenario, three actions are possible on a server, from
its record: **move** it to another cluster, **remove** it from a
cluster, or **add** it (a new hypothetical server). A variable's value
can also be overridden there. None of this changes reality — which is
what allows a scenario to be dropped and another one resumed without any
re-entry.

**Promoting** an adopted scenario moves its assumptions into reality:
hypothetical servers become real, moves and removals are applied,
variable overrides replace the real values. At the same time, the
competing scenarios that are explicitly chosen to be dropped are
closed — never guessed automatically.

Every screen that displays a state of the fleet (views, need/supply,
comparison) accepts being placed under a scenario, exactly as under
reality.

**The summary** ("Summary" button on each row) is a scenario's home page:
a table of all its clusters — those of its project, or all of them if it
has none — with, for each one, the limiting rule and its figures *before*
(real data) and *after* (scenario), the server movements (arrivals,
departures) and the cost of the added servers. Clusters still in deficit
under the scenario stand out, with a direct "Size" link. Two shortcuts at
the top of the page: **Compare with real data** opens the comparison
(§11) already configured — real data against this scenario, per cluster,
number of servers, costs and licenses — and **Batch sizing** leads to
processing all clusters at once (§10).

## 10. Reverse sizing

From a cluster's record, under a scenario: starting from a target need
for a given year, Parallax compares several candidate models from the
catalogue (in their latest revision) and shows, for each one, the number
of servers required, the acquisition cost, the annual cost, the
resulting capacity and the surplus relative to the need.

Before comparing, one chooses what, among the fleet already installed on
that cluster, is **kept** — generation by generation, not necessarily
all of it (some models may be obsolete or need to be reclaimed
elsewhere). The kept capacity is deducted from the need before
calculating how many new servers are required.

**Materializing** the chosen option creates, within the scenario, the
hypothetical servers linked to the chosen model and assigned to the
cluster, spread across the availability zones; installed servers that
are not kept are removed from the cluster within that same scenario. The
result is immediately visible in the delta on the need/supply screen.

**In batch.** From a scenario's summary, "Batch sizing" applies the same
mechanics to all its clusters at once: choose once the **kept
generations** (all checked = reinforcement, none checked = full renewal),
then a **candidate model per tier** — and, if needed, a model specific to
a cluster, which takes precedence. The **preview** shows, cluster by
cluster, what would be placed (servers to add, limiting rule, servers
removed, costs) and flags those that will be left aside: no model chosen,
or nothing to do. **Materialize the batch** places everything in a single
transaction — all or nothing — and returns to the summary, where the
before / after immediately shows the result.

## 11. Comparing two scenarios

"Simulation" menu → **Comparison**. Two scenarios (or a scenario and
reality) placed side by side, with the same choice of axes and
aggregates as the view builder, and a gap calculated automatically
between the two columns. Exportable like any other table.

## 12. The view builder

"Operations" menu → **Views**. The equivalent of a spreadsheet's pivot
tables:

- free, ordered **grouping axes** (project, environment, technology,
  tier, usage, cluster, zone, model, server status…);
- multiple-choice **filters** on each of these dimensions;
- **displayed columns** to choose from, with tags like the axes and in
  the desired order: number of servers, cores, RAM, disks (HDD/SSD kept
  separate), network, GPU, nodes, costs, license units and cost. By
  default: servers, cores, RAM, HDD, SSD, nodes.

A view is built as of a date of one's choosing (today by default) and
under a scenario of one's choosing. It can be saved to find again later,
shared with other users, and exported to CSV or Excel.

When a license column is requested without technology being a grouping
axis, Parallax automatically adds that axis: licenses from different
technologies are never totaled together.

Axes are chosen with **tags**: click or drag a tag into the zone to add
it as the last axis, drag within the zone to reorder, click a tag in the
zone to remove it — up to six axes. Among them, **Model year** groups by
generation (the annual order: DENSE-2025 → 2025), all types combined.
With several axes, axis cells with the same value are merged vertically
— a true hierarchical view, in views as in the comparison; exports stay
flat (repeated values), to remain usable in a spreadsheet. Clicking a
value column's header **sorts** (ascending, then descending) — within
each parent group, so the hierarchy stays readable; with a single axis,
it is a full sort. The sort is carried over by "Generate" and by the
exports.

## 13. Hardware requests

"Operations" menu → **Requests**. A text template, defined once by an
administrator ("Administration" menu → **Request template**), describes
how to build the description to paste into the company's hardware
request tool, from variables such as `{ip}`, `{vlan}`, `{cluster}`,
`{modele}`, `{cpu}`, `{ram}`… The full catalogue of available variables
is shown on the template screen.

The Requests screen lists the servers (filterable by scenario, project,
environment, cluster, status, with or without a request number), and for
each one: the generated text ready to copy, and the record number (the
server's reference within the request), editable directly in the table
with immediate recalculation of the text.

Once the request number has been obtained, a bulk action can assign it
to every server in a hypothesis (or to a filtered selection) in one go,
without going through servers one by one.

## 14. Software licenses

"Formula engine" menu → **Licenses**. A contract per technology and per
year (possibly overridden by scenario) defines how the units owed are
counted: by number of nodes installed, by RAM cap per machine, or by RAM
cap over an entire scope — with an associated unit cost. The result
appears as two columns in the view builder (units, cost), calculated
over the group rather than row by row.

A "global"-level contract is not additive by construction: the exact
figure is that of the entire fleet covered by the contract; a subtotal
by project, for example, remains an indicative breakdown, and the sum of
the subtotals can exceed the real total. This is flagged directly in the
views that display this kind of column.

## 15. IP addressing

"Inventory" menu → **VLAN**. A VLAN catalogue, each one defined by a
code, application criteria (project, environment, zone, cluster — a
criterion left blank applies to all), and one or more address ranges.

When a hypothesis becomes concrete (at the time of the quote request,
not before), the scenario screen offers to **address** in one action all
its hypothetical servers that don't yet have an address: Parallax infers
the applicable VLAN from the server's cluster and zone (or asks to
choose if more than one remains), and offers the first free address in
its ranges. The address pool is shared across all scenarios in progress:
two parallel quote requests will never be offered the same address. It
always remains possible to force a specific address directly on a
server's record.

**Network anomalies** ("Inventory" menu): a dedicated page flags,
without ever blocking anything, the inconsistencies detected — an
address carried by several servers, an address outside its VLAN's
ranges, a VLAN that doesn't match the server's cluster or zone, an
address with no VLAN. A marker also appears directly on the record and
row of the servers concerned.

## 16. Change log

"Administration" menu → **Log** (restricted to administrators). One row
per change: which record, which action (creation, modification,
correction, deletion), by whom, when, with the state before and after.
Filterable by entity, by user, or by period, and exportable. Each record
also displays its own history directly at the bottom of the page.

The log does not offer to automatically undo an action: it is a tool for
understanding a mistake, not for reverting without looking — which could
risk recreating an inconsistent state. Entries older than 800 days are
purged automatically.

## 17. Accounts and administration

"Administration" menu → **Accounts** (restricted to administrators):
create a local account, change a role, disable or re-enable an account,
reset a password. An account created automatically by a successful LDAP
sign-in has no local password — there is no point trying to reset it
from this screen, the corresponding checkbox is hidden.

**Backup** (restricted to administrators) configures an automatic local
copy of the database: destination folder, daily time, retention in days.
A new copy is written only if something changed since the previous one —
never a redundant file on a quiet night — and copies older than the
retention disappear on their own. A **Back up now** button lets you check
right away that the chosen folder works, without waiting for the
scheduled time.

## 18. Exporting any table

Every table displayed in Parallax — not just views — carries two
buttons, **CSV** and **Excel**, which export exactly what is shown on
screen, filters included.
