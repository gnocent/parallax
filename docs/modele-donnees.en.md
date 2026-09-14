# Parallax — Data model

*[Français](modele-donnees.md) · English*

> Technical reference for people developing on Parallax.
> For a functional presentation of the application, see the
> [user guide](guide-utilisateur.en.md).

Target: SQLite, WAL mode, concurrent access from 3-4 writers and 10-15 readers.
Volume: ~2000 servers, a few hundred models and rules. No performance
concern; the concern is the correctness of the model.

Conventions: integer technical identifiers, dates in `TEXT` ISO-8601 (`YYYY-MM-DD`),
a null `date_fin` meaning "still ongoing", a null `scenario_id` meaning "the real world".

---

# 1. Overview

```
                    ┌──────────────┐
                    │   scenario   │  (NULL = real)
                    └──────┬───────┘
                           │ overlay
       ┌───────────────────┼────────────────────┐
       │                   │                    │
┌──────▼──────┐    ┌───────▼────────┐   ┌───────▼────────┐
│ affectation │    │ variable_valeur│   │   contrainte   │
└──┬───────┬──┘    └───────┬────────┘   └───────┬────────┘
   │       │               │                    │
   │  ┌────▼─────┐         │                    │
   │  │ cluster  │◄────────┴────────────────────┘
   │  └────┬─────┘   (scope of the value / constraint)
   │       │
   │       │ filtered by
   │  ┌────▼─────┐        ┌───────────┐
   │  │  regle   │───────►│ metrique  │
   │  └──────────┘        └───────────┘
   │
┌──▼────────┐   ┌──────────────────┐   ┌────────┐
│  serveur  │──►│ serveur_revision │──►│ modele │
└───────────┘   └──────────────────┘   └───┬────┘
                                            │
                            ┌───────────────┼──────────────┐
                            │               │              │
                     ┌──────▼──────┐ ┌──────▼──────┐ ┌─────▼──────┐
                     │  revision   │ │  composant  │ │modele_noeud│
                     └─────────────┘ └─────────────┘ └────────────┘
```

---

# 2. Reference data

```sql
CREATE TABLE projet (
  id          INTEGER PRIMARY KEY,
  code        TEXT NOT NULL UNIQUE,      -- LOGS, STREAM, SECOPS
  libelle     TEXT NOT NULL,
  actif       INTEGER NOT NULL DEFAULT 1
);

CREATE TABLE environnement (
  id          INTEGER PRIMARY KEY,
  code        TEXT NOT NULL UNIQUE,      -- DEV, INT, QUAL, PREPROD, PROD
  libelle     TEXT NOT NULL,
  ordre       INTEGER NOT NULL           -- for display
);

CREATE TABLE techno (
  id          INTEGER PRIMARY KEY,
  code        TEXT NOT NULL UNIQUE,      -- ELASTIC, KAFKA, NOMAD, NOMAD, LOGSTASH, MINIO
  libelle     TEXT NOT NULL
);

CREATE TABLE tier (
  id          INTEGER PRIMARY KEY,
  code        TEXT NOT NULL UNIQUE,      -- HOT, WARM, COLD, ML
  libelle     TEXT NOT NULL,
  ordre       INTEGER NOT NULL
);

CREATE TABLE usage_fonctionnel (
  id          INTEGER PRIMARY KEY,
  code        TEXT NOT NULL UNIQUE,      -- LOGMGMT, MONITORING, CYBER
  libelle     TEXT NOT NULL
);

CREATE TABLE zone (
  id          INTEGER PRIMARY KEY,
  code        TEXT NOT NULL UNIQUE,
  libelle     TEXT NOT NULL,
  site        TEXT
);
```

---

# 3. Scope: the cluster

Central entity. A cluster is the concrete intersection of the dimensions: it is
what servers are assigned to and what rules target.

```sql
CREATE TABLE cluster (
  id                    INTEGER PRIMARY KEY,
  nom                   TEXT NOT NULL,          -- ElasticCold1
  projet_id             INTEGER NOT NULL REFERENCES projet(id),
  environnement_id      INTEGER NOT NULL REFERENCES environnement(id),
  techno_id             INTEGER NOT NULL REFERENCES techno(id),
  tier_id               INTEGER REFERENCES tier(id),               -- NULL allowed
  usage_fonctionnel_id  INTEGER REFERENCES usage_fonctionnel(id),  -- NULL allowed
  commentaire           TEXT,
  actif                 INTEGER NOT NULL DEFAULT 1,
  UNIQUE (projet_id, environnement_id, nom)
);
```

> **Note.** Tier and usage are optional: Kafka and Nomad have no tier;
> usage only distinguishes cases where the same technology serves several
> functional domains within the same project.

---

# 4. Hardware catalogue

## 4.1 Model and revisions

```sql
CREATE TABLE modele (
  id                  INTEGER PRIMARY KEY,
  type                TEXT NOT NULL,            -- DENSE
  annee               INTEGER NOT NULL,         -- 2025
  code                TEXT NOT NULL UNIQUE,     -- DENSE-2025 (historical join key)
  description         TEXT,
  mode_financement    TEXT,                     -- ACHAT | LEASE
  duree_lease_mois    INTEGER,
  date_debut_lease    TEXT,
  prix_fournisseur_ht REAL,                     -- vendor quote, upfront estimate
  cout_annuel_ht      REAL,                     -- infra quote, constant over the term
  duree_cout_annees   INTEGER,                  -- 5 or 6
  actif               INTEGER NOT NULL DEFAULT 1
);

-- A revision is IMMUTABLE once created and referenced.
-- Any actual hardware change creates a new revision.
-- Correcting an error is a distinct action in the UI, with a warning.
CREATE TABLE revision (
  id            INTEGER PRIMARY KEY,
  modele_id     INTEGER NOT NULL REFERENCES modele(id),
  numero        INTEGER NOT NULL,               -- 1, 2, 3...
  libelle       TEXT,                           -- "added 8 3.84 TB SSDs"
  date_effet    TEXT NOT NULL,
  commentaire   TEXT,
  UNIQUE (modele_id, numero)
);
```

## 4.2 Components

Replaces the precomputed CPU / RAM / DISK_70 columns. The calculation is
deferred to the formulas, which preserves the raw capacity and keeps the
coefficients adjustable.

```sql
CREATE TABLE composant (
  id                 INTEGER PRIMARY KEY,
  revision_id        INTEGER NOT NULL REFERENCES revision(id),
  nature             TEXT NOT NULL,   -- CPU | RAM | DISQUE_DATA | GPU | NIC | AUTRE
  code               TEXT NOT NULL,   -- variable exposed to the formulas: ssd, cpu, ram
  quantite           REAL NOT NULL,   -- 24 disks, 2 cards, 4 GPUs; 1 for a scalar
  capacite_unitaire  REAL NOT NULL,   -- 24 (TB), 25 (Gbps); 64 cores, 1024 GB for a scalar
  unite              TEXT NOT NULL,   -- TO | GO | CORE | GBPS | POINT | TFLOPS | TOS | GOS | MOS | UNITE
  commentaire        TEXT,
  UNIQUE (revision_id, code)
);
```

**Canonical vocabulary.** The code is free-form in the database, but the
import of models, the view builder and scenario comparison only recognize
the codes below (`depot.ComposantsCanoniques`). A code outside the list
(nature AUTRE) remains usable in formulas, not in view columns. Two forms:

- **scalar**: quantity 1, the unit capacity carries the server's total. It
  is not broken down into sockets or memory sticks — only the total
  matters for the calculation, and CPU power scores have no physical unit;
- **counted**: quantity × unit capacity, the elements are assumed identical
  (network cards, disks of the same family, GPUs).

| code | nature | form | quantity | unit capacity | unit |
|---|---|---|---|---|---|
| `cpu` | CPU | scalar | 1 | total number of cores | CORE |
| `specrate` | CPU | scalar | 1 | base SPECrate score | POINT |
| `phoronix` | CPU | scalar | 1 | Phoronix Test Suite score | POINT |
| `ram` | RAM | scalar | 1 | total GB | GO |
| `hdd` | DISQUE_DATA | counted | number of disks | TB per disk | TO |
| `ssd` | DISQUE_DATA | counted | number of disks | TB per disk | TO |
| `nic` | NIC | counted | number of cards | Gbps per card | GBPS |
| `gpu` | GPU | counted | number of GPUs | 1 | UNITE |
| `gpu_ram` | GPU | counted | number of GPUs | GB per GPU | GO |
| `gpu_fp8` | GPU | counted | number of GPUs | FP8 TFLOPS per GPU | TFLOPS |
| `gpu_bp` | GPU | counted | number of GPUs | memory bandwidth per GPU (TB/s) | TOS |

The three GPU attributes repeat the quantity of the `gpu` component: this
is the only redundancy allowed, because each attribute must remain a
distinct formula variable (`gpu_ram_total`, `gpu_fp8_machine`…). System
disks are not described: they enter into no capacity calculation.

GOS (GB/s) and MOS (MB/s, migration `0008`) are not the unit of any
canonical code — a disk throughput, for instance, is declared as an `AUTRE`
component (free code, e.g. `hdd_debit`): visible to formulas, not to view
columns or import, like any code outside this vocabulary.

Example for a storage server:

| nature | code | quantite | capacite_unitaire | unite |
|---|---|---|---|---|
| CPU | `cpu` | 1 | 64 | CORE |
| CPU | `specrate` | 1 | 480 | POINT |
| RAM | `ram` | 1 | 1024 | GO |
| DISQUE_DATA | `hdd` | 24 | 24 | TO |
| DISQUE_DATA | `ssd` | 2 | 3.84 | TO |
| NIC | `nic` | 2 | 25 | GBPS |

Formulas have access to `hdd_total = 576`, and the historical calculation
`24 × 24 × 0.9 × 0.7 = 362.88` is written as
`hdd_total * coef_formatage * taux_remplissage_max`.

This vocabulary replaced, on 2026-09-11, the original breakdown (sockets ×
cores, memory sticks × GB, system disks). Migration `0001` was modified in
place for the `nature` and `unite` enumerations — an exception knowingly
made to the rule "never modified once applied": no deployed instance
existed, databases start empty.

## 4.3 Installable nodes per technology

```sql
CREATE TABLE modele_noeud (
  id            INTEGER PRIMARY KEY,
  revision_id   INTEGER NOT NULL REFERENCES revision(id),
  techno_id     INTEGER NOT NULL REFERENCES techno(id),
  nb_noeuds     INTEGER NOT NULL,
  UNIQUE (revision_id, techno_id)
);
```

---

# 5. Servers

```sql
CREATE TABLE serveur (
  id                INTEGER PRIMARY KEY,     -- technical identity, the only stable key
  physical_name     TEXT,                    -- an attribute, NOT an identity
  hostname          TEXT,
  serial_number     TEXT,
  zone_id     INTEGER REFERENCES zone(id),
  position_zone       TEXT,                    -- documentary
  ip                TEXT,                    -- production IP only
  vlan_id           INTEGER REFERENCES vlan(id),
  version_os        TEXT,
  typologie    TEXT,                    -- P | A | D | PA | AD | PAD, entered, expected by the request tool
  code_appli        TEXT,
  demande_ref     TEXT,
  demande_serveur_ref         TEXT,
  statut            TEXT NOT NULL,           -- HYPOTHESE|COMMANDE|EN_SERVICE|DECOMMISSIONNE
  scenario_id       INTEGER REFERENCES scenario(id),  -- NULL = real
  date_entree       TEXT,
  date_sortie       TEXT,
  commentaire       TEXT
);

CREATE INDEX idx_serveur_scenario ON serveur(scenario_id);
CREATE INDEX idx_serveur_statut   ON serveur(statut);
```

> **Hypothetical servers.** A server created within a scenario carries
> `scenario_id`. Promoting a scenario means setting `scenario_id` to NULL
> and the status to `COMMANDE`. No data is destroyed.

## 5.1 Attachment to the model, with revision and dates

```sql
CREATE TABLE serveur_revision (
  id           INTEGER PRIMARY KEY,
  serveur_id   INTEGER NOT NULL REFERENCES serveur(id),
  revision_id  INTEGER NOT NULL REFERENCES revision(id),
  date_debut   TEXT NOT NULL,
  date_fin     TEXT,                          -- NULL = ongoing
  commentaire  TEXT
);

CREATE INDEX idx_serveur_revision ON serveur_revision(serveur_id, date_debut);
```

Makes it possible to know the hardware characteristics of a server **at any
past date**, without bitemporality, thanks to the immutability of revisions.

## 5.2 Assignment

```sql
CREATE TABLE affectation (
  id           INTEGER PRIMARY KEY,
  serveur_id   INTEGER NOT NULL REFERENCES serveur(id),
  cluster_id   INTEGER NOT NULL REFERENCES cluster(id),
  date_debut   TEXT NOT NULL,
  date_fin     TEXT,                          -- NULL = ongoing
  scenario_id  INTEGER REFERENCES scenario(id),  -- NULL = real
  commentaire  TEXT
);

CREATE INDEX idx_affectation_serveur  ON affectation(serveur_id, date_debut);
CREATE INDEX idx_affectation_cluster  ON affectation(cluster_id, date_debut);
CREATE INDEX idx_affectation_scenario ON affectation(scenario_id);
```

A server with no active assignment is a natural candidate for reuse. A
reassignment between projects is simply the end of one assignment followed
by a new one — including within a scenario.

## 5.3 Network

```sql
CREATE TABLE vlan (
  id                INTEGER PRIMARY KEY,
  code              TEXT NOT NULL UNIQUE,
  projet_id         INTEGER REFERENCES projet(id),          -- applicability criteria:
  environnement_id  INTEGER REFERENCES environnement(id),   -- NULL = all
  zone_id           INTEGER REFERENCES zone(id),
  cluster_id        INTEGER REFERENCES cluster(id),         -- v3.1
  derniere_ip       TEXT,                                   -- allocation cursor (v3.1)
  commentaire       TEXT
);

CREATE TABLE plage_ip (
  id        INTEGER PRIMARY KEY,
  vlan_id   INTEGER NOT NULL REFERENCES vlan(id),
  ip_debut  TEXT NOT NULL,   -- closed intervals; gateway and reserved addresses fall outside the interval
  ip_fin    TEXT NOT NULL
);
```

Addresses as IPv4 text, compared numerically. The pool is global: an
address is taken as soon as a non-decommissioned server carries it, real or
hypothetical from a non-abandoned scenario — allocation only happens when a
hypothesis is made concrete (a quote request), never at its creation,
hence with no per-scenario overlay. Suggestion: the first free address
after `derniere_ip` within the VLAN's ranges, wrapping around; the user can
force any address. Inconsistencies (duplicate address, out of range, VLAN
incompatible with the criteria) are not database constraints: they are
computed at read time and flagged (v3.1).

---

# 6. Scenarios

```sql
CREATE TABLE scenario (
  id             INTEGER PRIMARY KEY,
  nom            TEXT NOT NULL,
  projet_id      INTEGER REFERENCES projet(id),   -- attachment, not restriction
  description    TEXT NOT NULL,                   -- mandatory: people come back to it months later
  statut         TEXT NOT NULL,                   -- BROUILLON|ACTIF|RETENU|ABANDONNE
  date_creation  TEXT NOT NULL,
  date_cloture   TEXT,
  auteur_id      INTEGER REFERENCES utilisateur(id)
);
```

No derivation between scenarios: a single generation of overlays. The
attachment to a project is used for classification; deltas can involve
servers from any project, to allow hypotheses of release and reassignment
between projects.

**Resolving assignments under a scenario.** The bucket
(`COALESCE(scenario_id, -1)`) guarantees the uniqueness of the active
assignment *within each bucket taken in isolation* (invariant 3), but says
nothing about what a screen should show when the same server has an active
assignment both in the real world *and* another in the scenario. The rule,
symmetric to the one for variables and constraints (§7.2, §8) —
overlay, never merge — is: **for a given server, if there is at least one
assignment row in the scenario's bucket, that row (or the absence of an
active row on the date in question) *entirely* overrides the real world for
that server.** Concretely:

- **moving** a real server into a scenario: an assignment to the new
  cluster, opened in the scenario's bucket. The real world is left
  untouched; under the scenario, the server no longer appears in its real
  cluster.
- **removing** a real server in a scenario, without reassigning it
  elsewhere: open then close an assignment in the scenario's bucket (the
  same `Affecter`/`Desaffecter` actions as in the real world). The scenario
  then carries a row for that server which is active on no date — which is
  enough to make the overlay win, without needing a separate representation
  of a "removal" or a nullable `cluster_id`.
- **adding** a hypothetical server: no real row to override, the
  scenario's row applies directly.

`internal/depot.AffectationsResolues` implements this resolution;
`ListerAffectationsCluster` remains the raw read (real plus scenario, with
no arbitration), useful for listing the deltas themselves rather than the
resulting state.

**Promotion.** It applies to the real world exactly what the read showed —
not a simple flip of `scenario_id` to NULL, which would leave a moved
server with two open assignments (rejected by
`idx_affectation_active_unique`) and an overridden variable with two real
values at the same scope. For each affected server: the current real
assignment is closed the day before the move, or on the date of the
removal (the removal's clone row, a technical artifact, is merged into the
real row it copied); the scenario's rows then join the real world, after
checking that they overlap no real lifespan. A move dated no later than the
start of the current real assignment is rejected (`ErrChevauchement`,
naming the server): it would require rewriting history. A variable or
constraint overlay at a scope where the real world already has a value
overwrites it (with history kept for variables) instead of duplicating it.
See `depot.Promouvoir`.

---

# 7. Variables

## 7.1 Declaration

```sql
CREATE TABLE variable (
  id        INTEGER PRIMARY KEY,
  code      TEXT NOT NULL UNIQUE,   -- debit_jour_to, retention_jours, taux_compression
  libelle   TEXT NOT NULL,
  unite     TEXT,
  defaut    REAL,
  commentaire TEXT
);
```

## 7.2 Values, resolved by (scenario, year) and scope

```sql
CREATE TABLE variable_valeur (
  id                    INTEGER PRIMARY KEY,
  variable_id           INTEGER NOT NULL REFERENCES variable(id),
  scenario_id           INTEGER REFERENCES scenario(id),   -- NULL = real
  annee                 INTEGER NOT NULL,
  -- scope: from most general to most specific, all columns nullable
  projet_id             INTEGER REFERENCES projet(id),
  environnement_id      INTEGER REFERENCES environnement(id),
  techno_id             INTEGER REFERENCES techno(id),
  tier_id               INTEGER REFERENCES tier(id),
  cluster_id            INTEGER REFERENCES cluster(id),
  valeur                REAL NOT NULL,
  commentaire           TEXT,
  modifie_le            TEXT NOT NULL,
  modifie_par           INTEGER REFERENCES utilisateur(id)
);

CREATE INDEX idx_varval ON variable_valeur(variable_id, scenario_id, annee);

-- Safety history against accidental overwrites
CREATE TABLE variable_valeur_historique (
  id            INTEGER PRIMARY KEY,
  valeur_id     INTEGER NOT NULL,
  ancienne      REAL NOT NULL,
  nouvelle      REAL NOT NULL,
  horodatage    TEXT NOT NULL,
  utilisateur_id INTEGER REFERENCES utilisateur(id)
);
```

**Resolution rule** — for a given (cluster, year, scenario):

1. look in the scenario, otherwise in the real world;
2. for equal scenario standing, keep the value whose scope is the **most
   specific** among those that apply (cluster > tier > techno >
   environnement > projet > global);
3. failing that, the variable's default value.

This resolution is what allows the daily throughput to be entered once at
project / environment level while specializing retention by tier.

---

# 8. Capacity rules

```sql
CREATE TABLE metrique (
  id       INTEGER PRIMARY KEY,
  code     TEXT NOT NULL UNIQUE,   -- CPU_CORES, DISQUE_UTILE_TO, RAM_GO, NOEUDS, LICENCES
  libelle  TEXT NOT NULL,
  unite    TEXT NOT NULL
);

CREATE TABLE regle (
  id                  INTEGER PRIMARY KEY,
  nom                 TEXT NOT NULL,
  metrique_id         INTEGER NOT NULL REFERENCES metrique(id),
  expression          TEXT NOT NULL,     -- evaluated by the expression engine
  niveau_evaluation   TEXT NOT NULL,     -- PERIMETRE | PAR_SERVEUR
  composant_offre     TEXT,              -- code of the component providing the metric
  -- filter defining the scope of application, nullable columns
  projet_id             INTEGER REFERENCES projet(id),
  environnement_id      INTEGER REFERENCES environnement(id),
  techno_id             INTEGER REFERENCES techno(id),
  tier_id               INTEGER REFERENCES tier(id),
  usage_fonctionnel_id  INTEGER REFERENCES usage_fonctionnel(id),
  cluster_id            INTEGER REFERENCES cluster(id),
  actif               INTEGER NOT NULL DEFAULT 1,
  commentaire         TEXT,
  date_creation       TEXT NOT NULL
);
```

**Invariant to be enforced by the application**: two active rules bearing
on the same metric must not have filters that overlap, meaning they must
not match the same cluster. Checked at save time, explicit refusal on
conflict, naming the competing rule. This removes any implicit priority
rule.

Rules are **duplicable** and **can be deactivated** — this is how a rule
evolves from one year to the next without losing the previous one.

## 8.1 Evaluation levels

| Level | Semantics | Example |
|---|---|---|
| `PERIMETRE` | the formula is evaluated once on the cluster's aggregates | `ceil(ram_totale / ram_max_licence)` |
| `PAR_SERVEUR` | the formula is evaluated per server then summed | `sum(ceil(ram_machine / ram_max_licence))` |

Both are necessary: the two licensing mechanisms encountered each require
one.

## 8.2 Licenses (v3.3)

Licenses are not a rule per cluster: the contract is counted over a scope
chosen in a view, and some levels are not additive. One contract per
technology and per year, overridable by scenario.

```sql
CREATE TABLE licence_contrat (
  id               INTEGER PRIMARY KEY,
  techno_id        INTEGER NOT NULL REFERENCES techno(id),
  annee            INTEGER NOT NULL,
  scenario_id      INTEGER REFERENCES scenario(id),   -- NULL = real
  mecanisme        TEXT NOT NULL,    -- NOEUDS | MAX_NOEUDS_RAM | RAM
  niveau           TEXT NOT NULL,    -- MACHINE | CLUSTER | GLOBAL (not applicable to NOEUDS)
  ram_max_go       REAL,             -- required except for NOEUDS
  cout_unitaire_ht REAL,
  commentaire      TEXT,
  UNIQUE (techno_id, annee, COALESCE(scenario_id, -1))
);
```

Resolution for a view at a given date: the contract for the date's year,
failing that the most recent of the prior years (a contract runs until the
next one). Calculation over a group in the view, by technology:

| mechanism | MACHINE | CLUSTER | GLOBAL |
|---|---|---|---|
| `NOEUDS` | Σ nb_noeuds | same | same |
| `MAX_NOEUDS_RAM` | Σ max(nb_noeuds, ceil(ram / ram_max)) per server | Σ max(Σ nb_noeuds, ceil(Σ ram / ram_max)) per cluster | max(Σ nb_noeuds, ceil(Σ ram / ram_max)) |
| `RAM` | Σ ceil(ram / ram_max) per server | Σ ceil(Σ ram / ram_max) per cluster | ceil(Σ ram / ram_max) |

`nb_noeuds`: `modele_noeud` of the server's revision for the cluster's
technology; `ram`: the `ram` component. Cost = units × unit cost.

---

# 9. Sizing constraints

Scoped by cluster, and indexed by (scenario, year) like variables — the
number of zones in a cluster can change from one year to the next, and
that is precisely the kind of hypothesis meant to be compared.

```sql
CREATE TABLE contrainte (
  id           INTEGER PRIMARY KEY,
  cluster_id   INTEGER NOT NULL REFERENCES cluster(id),
  scenario_id  INTEGER REFERENCES scenario(id),   -- NULL = real
  annee        INTEGER NOT NULL,
  type         TEXT NOT NULL,
  -- MIN_TOTAL | MIN_PAR_ZONE | MULTIPLE_TOTAL | MULTIPLE_PAR_ZONE
  -- | NB_ZONES | EQUILIBRAGE_ZONE
  valeur       REAL,
  portee       TEXT,                              -- NOUVEAUX | TOUS (for EQUILIBRAGE_ZONE)
  commentaire  TEXT
);
```

## 9.1 Sizing algorithm

For a cluster, a year, a scenario and a candidate model:

```
1. rules ← active rules whose filter matches the cluster
2. for each rule r:
     need[r]     = eval(r.expression, resolved variables)
     unit_supply = value of the r.composant_offre component in the candidate model
     raw_count[r] = need[r] / unit_supply
3. count = max(raw_count)                ← the winning rule is the limiting factor
4. apply the cluster's constraints, in order:
     ceil, MIN_TOTAL, MULTIPLE_TOTAL, then per-zone distribution and constraints
5. return count, the limiting rule, the cost, the resulting capacity and the surplus
```

Order matters: rounding each rule before the `max` would give a different,
wrong result.

Repeated over several candidate models, this algorithm produces the
comparison table used to find the technical-economic balance.

---

# 10. Saved views

```sql
CREATE TABLE vue (
  id           INTEGER PRIMARY KEY,
  nom          TEXT NOT NULL,
  proprietaire INTEGER REFERENCES utilisateur(id),
  partagee     INTEGER NOT NULL DEFAULT 0,
  axes         TEXT NOT NULL,   -- JSON: ordered grouping axes
  filtres      TEXT NOT NULL,   -- JSON
  colonnes     TEXT NOT NULL,   -- JSON: displayed aggregates
  date_creation TEXT NOT NULL
);
```

Equivalent of today's pivot tables. Any view can be exported to Excel. A
view is read as of a date (v3.0, defaults to today): assignments and
revisions resolved at that date. License columns (§8.2) are computed over
the group, not by summing rows; when requested without a techno axis, the
table is split by technology.

## 10.1 Application parameters

```sql
CREATE TABLE parametre (
  cle         TEXT PRIMARY KEY,   -- gabarit_demande
  valeur      TEXT NOT NULL,
  modifie_le  TEXT NOT NULL,
  modifie_par INTEGER REFERENCES utilisateur(id)
);
```

`gabarit_demande`: free text with line breaks and `{nom}` variables (`{{`
for a literal brace), rendered for each server — the catalogue of
variables is shown on the screen (`/parametres/gabarit`) and described in
the [user guide](guide-utilisateur.en.md#13-hardware-requests). A single
template for the whole application, edited by an administrator; an
unknown variable is rejected at entry, a missing value is rendered empty.

`sauvegarde_dossier`, `sauvegarde_heure` (`HH:MM`), `sauvegarde_retention_jours`:
settings for the automatic local backup (backlog v3.7,
`/parametres/sauvegarde`, `docs/deploiement.md` §6.1 bis); empty or absent,
the feature stays disabled. `sauvegarde_derniere_reussite` (RFC 3339) and
`sauvegarde_derniere_verification` (`YYYY-MM-DD`) are internal state
written by the scheduler (`cmd/parallax`), never entered on the screen.
`depot.ActiviteDepuis` excludes the whole `parametre` entity from the
journal (not just these two keys): counting them would create a loop where
every check declares itself as a change.

---

# 11. Users and traceability

The `journal` table (below) is fed on every write: one row per modified
entity, `avant` and `apres` as the JSON of the entire row, written within
the transaction of the change. No automatic restore from the journal — see
the [user guide](guide-utilisateur.en.md#16-change-log) for the
justification. Purged beyond 800 days.

```sql
CREATE TABLE utilisateur (
  id        INTEGER PRIMARY KEY,
  login     TEXT NOT NULL UNIQUE,
  hash      TEXT NOT NULL,        -- argon2id
  nom       TEXT,
  role      TEXT NOT NULL,        -- ADMIN | EDITEUR | LECTEUR
  actif     INTEGER NOT NULL DEFAULT 1,
  cree_le   TEXT NOT NULL
);

CREATE TABLE journal (
  id             INTEGER PRIMARY KEY,
  entite         TEXT NOT NULL,
  entite_id      INTEGER NOT NULL,
  action         TEXT NOT NULL,   -- CREATION | MODIFICATION | CORRECTION | SUPPRESSION
  utilisateur_id INTEGER REFERENCES utilisateur(id),
  horodatage     TEXT NOT NULL,
  avant          TEXT,            -- JSON
  apres          TEXT             -- JSON
);
```

Journal deliberately kept light, with no regulatory ambition. It mainly
serves to understand who changed what when a figure comes as a surprise.

---

# 12. Invariants to be enforced

1. A **revision referenced by a `serveur_revision` is immutable**: any
   change creates a new revision. Correcting an error is a distinct UI
   action, accompanied by a warning.
2. **No overlap of active rules** on the same metric.
3. A server has **only one active assignment** at a time within a given
   scenario.
4. The `serveur_revision` periods of a given server **do not overlap**.
5. A server with status `HYPOTHESE` must carry a `scenario_id`.
6. Promoting a scenario deletes nothing: it flips `scenario_id` to NULL
   and closes competing scenarios as `ABANDONNE`.
7. An IP address is only offered if it is consumed neither in the real
   world nor in the current scenario.

---

# 13. What this schema replaces in an inventory spreadsheet

| Typical spreadsheet column | Becomes |
|---|---|
| project, environment, usage | dimensions of the `cluster` (`projet_id`, `environnement_id`, `techno_id` / `tier_id` / `usage_fonctionnel_id`) |
| hardware type and year | `modele.code`, via `serveur_revision` |
| lease end | derived from `modele.date_debut_lease` + `duree_lease_mois` |
| CPU, RAM, disks, network, GPU, precomputed "usable" capacity | derived from the revision's `composant` rows + variables |
| number of nodes | `modele_noeud` (per technology) |
| vendor price, annual cost | `modele.prix_fournisseur_ht`, `modele.cout_annuel_ht` |
| comment to paste into the request tool | generated on demand (v3.2) |
| reuse target, decommissioning confirmation | replaced by dated `affectation` + `serveur.statut` |
| hardware description | `modele.description` |
