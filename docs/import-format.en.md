# Parallax — import file format

*[Français](import-format.md) · English*

Self-contained reference for the four CSV imports (`/import/referentiels`,
`/import/clusters`, `/import/modeles`, `/import/serveurs`), to prepare files
before even starting the application. The format is also documented,
identically, directly on each page once the application is running.

**Order**: reference data, clusters, models, servers, then VLANs — each file
references by code what the previous ones created. The five example files
(`referentiels-exemple.csv`, `clusters-exemple.csv`, `modeles-exemple.csv`,
`serveurs-exemple.csv`, `vlans-exemple.csv`) import in this order into an
empty database and produce a consistent data set. Embedded in the binary
(`internal/web/exemples/`), they can be downloaded directly from the
application's `/import` screen, with no need for the repository.

Common rules: **semicolon** separator (`;`), first line = headers, columns
in any order, unknown columns ignored, Excel BOM tolerated. An import
creates, it never updates: a code already present in the database is a row
error.

Each import happens in two steps in the application: **Analyze** (no write,
just a row-by-row report) then **Confirm** — and if a single row is invalid,
nothing at all is written, never a partial import.

---

## Reference data import

A single file for the six tables; the `type` column distinguishes the rows.

**Required**: `type` (`PROJET`, `ENVIRONNEMENT`, `TECHNO`, `TIER`,
`USAGE` or `ZONE`), `code`, `libelle`

**Optional**: `ordre` (integer, display order — read for `ENVIRONNEMENT` and
`TIER`, ignored elsewhere), `site` (read for `ZONE`, ignored elsewhere).
Projects and technologies created are active.

```csv
type;code;libelle;ordre;site
PROJET;LOGS;Plateforme de logs;;
ENVIRONNEMENT;PROD;Production;1;
ENVIRONNEMENT;PREPROD;Pré-production;2;
TECHNO;ELASTIC;Elasticsearch;;
TIER;HOT;Données chaudes;1;
USAGE;INGEST;Ingestion;;
ZONE;AZ1;Zone 1;;Site A
```

## Cluster import

**Required**: `nom`, `projet_code`, `environnement_code`, `techno_code`
(codes of existing reference data)

**Optional**: `tier_code`, `usage_code`, `commentaire`

A cluster is unique by (project, environment, name).

```csv
nom;projet_code;environnement_code;techno_code;tier_code;usage_code;commentaire
ElasticHot;LOGS;PROD;ELASTIC;HOT;;Indexation temps réel
ElasticCold1;LOGS;PROD;ELASTIC;COLD;;
Kafka;LOGS;PROD;KAFKA;;INGEST;
```

## Model import

**Required**: `type`, `annee`, `code`

**Optional**: `description`, `mode_financement` (`ACHAT` or `LEASE`),
`duree_lease_mois`, `date_debut_lease` (`YYYY-MM-DD`), `prix_fournisseur_ht`,
`cout_annuel_ht`, `duree_cout_annees`. Decimal numbers accept either a comma
or a period.

**Components** (all optional — see the canonical vocabulary,
`docs/modele-donnees.md` §4.2). Simple values are per-server totals; count /
unit-capacity pairs are filled in together, the elements of a pair being
assumed identical:

| Columns | Component created |
|---|---|
| `cpu_coeurs` | CPU, code `cpu` — total number of cores |
| `specrate` | CPU, code `specrate` — SPECrate base score |
| `phoronix` | CPU, code `phoronix` — Phoronix Test Suite score |
| `ram_go` | RAM, code `ram` — total GB |
| `hdd_nb` + `hdd_to` | DISQUE_DATA, code `hdd` — number of disks × TB per disk |
| `ssd_nb` + `ssd_to` | DISQUE_DATA, code `ssd` — number of disks × TB per disk |
| `nic_nb` + `nic_gbps` | NIC, code `nic` — number of cards × Gbps per card |
| `gpu_nb` | GPU, code `gpu` — number of GPUs |
| `gpu_nb` + `gpu_ram_go` | GPU, code `gpu_ram` — GB per GPU |
| `gpu_nb` + `gpu_tflops_fp8` | GPU, code `gpu_fp8` — TFLOPS FP8 per GPU |
| `gpu_nb` + `gpu_bp_tos` | GPU, code `gpu_bp` — memory bandwidth per GPU, in TB/s |

The three GPU attributes require `gpu_nb`; `gpu_nb` alone is accepted
(number of GPUs with no characteristics). System disks are not imported:
they enter into no calculation.

If at least one component is filled in on a row, a revision (number 1,
labeled "initial import") is created for the model and carries these
components.

Example:

```csv
type;annee;code;mode_financement;cpu_coeurs;specrate;ram_go;hdd_nb;hdd_to;ssd_nb;ssd_to;nic_nb;nic_gbps
DENSE;2025;DENSE-2025;LEASE;64;480;1024;24;24;2;3,84;2;25
LEGACY;2024;LEGACY-2024;ACHAT;48;310;512;;;8;3,84;2;10
```

A complete file, including a GPU node: downloadable from `/import` in the
application, or at `internal/web/exemples/modeles-exemple.csv`.

## VLAN and range import (v3.1)

**Required**: `code`. **Optional**: `projet_code`,
`environnement_code`, `zone_code`, `cluster_nom` (with `projet_code` and
`environnement_code` on the same row), `ip_debut`, `ip_fin`,
`commentaire`. An empty criterion means "all". One row per range; the VLAN
is created once for its code, with the criteria of its first row (different
criteria across rows sharing the same code is an error). A row with no
`ip_debut`/`ip_fin` creates the VLAN alone. Gateway and reserved addresses
are left outside the range.

```csv
code;projet_code;environnement_code;zone_code;cluster_nom;ip_debut;ip_fin;commentaire
LOGS-PROD-DC1;LOGS;PROD;DC1;;10.10.1.10;10.10.1.250;
LOGS-PROD-DC2;LOGS;PROD;DC2;;10.10.2.10;10.10.2.250;
```

Example file: downloadable from `/import`, or at
`internal/web/exemples/vlans-exemple.csv`; to be imported after the servers
(addressing anomalies are computed afterward, without blocking).

## Bulk server update (v3.5)

Page `/import/serveurs/maj`. Modifies **existing real servers**; never
creates one (the creation import refuses known ones, this one refuses
unknown ones).

**Matching key**, in this order, the first that identifies exactly one
server: `hostname`; otherwise `demande_ref` +
`demande_serveur_ref`; otherwise `physical_name`. Ambiguous or unknown is an
error.

**Adaptive file**: only the columns present are touched; an empty cell
changes nothing; `#VIDE` clears it. Recognized columns:
`physical_name`, `hostname`, `serial_number`, `zone_code`, `position_zone`,
`ip`, `vlan_code`, `version_os`, `typologie`, `code_appli`, `demande_ref`,
`demande_serveur_ref`, `commentaire`, `statut` (COMMANDE, EN_SERVICE,
DECOMMISSIONNE), `projet_code` + `environnement_code` + `cluster_nom` +
`date_affectation` (required if the cluster changes), `modele_code` +
`date_rattachement` (required if the model changes).

The analysis shows, per row, "field: before → after"; the write, after
confirmation, is a single transaction and each change is logged under the
user's name. "Export for update" on the server list produces the exact
format, filters included.

```csv
demande_ref;demande_serveur_ref;hostname;serial_number;position_zone;statut
DEM-2026-00042;SRV-201;esh04;9Q2W7E1;Baie 14 U05-U07;EN_SERVICE
```

## Server import

**Required**: `physical_name`, `statut` (`COMMANDE`, `EN_SERVICE` or
`DECOMMISSIONNE` — `HYPOTHESE` requires a scenario, not supported on
import), `date_entree` (`YYYY-MM-DD`)

**Optional — attributes**: `hostname`, `serial_number`,
`zone_code` (must already exist in the application), `position_zone`,
`ip`, `version_os`, `typologie`, `code_appli`, `demande_ref`,
`demande_serveur_ref`, `commentaire`

**Optional — assignment**: `projet_code`, `environnement_code` and
`cluster_nom` together identify a cluster already created in the
application. **All three must be filled in, or none** — just one or two is
a row error. `date_affectation` optional, defaulting to `date_entree`.

**Optional — revision attachment**: `modele_code` (must already exist and
carry at least one revision — automatically resolves to the most recent
revision of that model). `date_rattachement` optional, defaulting to
`date_entree`.

Example:

```csv
physical_name;statut;date_entree;zone_code;projet_code;environnement_code;cluster_nom;modele_code
PHY00123;EN_SERVICE;2023-03-01;DC1;LOGS;PROD;ElasticHot1;DENSE-2023
PHY00124;EN_SERVICE;2023-03-01;DC1;LOGS;PROD;ElasticHot1;DENSE-2023
PHY00200;COMMANDE;2024-11-15;;;;;
```

(the third row: a server on order, with no zone, no assignment and no
attachment — any optional field can stay empty)

## Starting from an existing inventory

A typical inventory spreadsheet holds one row per server with hardware
columns repeated (CPU, RAM, disks, price). Parallax separates them: what
describes a hardware generation goes into the model import (once per model,
not per server), what describes a server goes into the server import, and
what is a calculation dimension (project, environment, technology, tier,
usage) becomes a cluster to create beforehand. Precomputed usable capacities
("disk at 70%") are not imported: they are recalculated by the rules.
Manual tracking columns (reuse target, decommissioning confirmation) are
replaced by the server's dated assignment and status.

## After the import

The decisive milestone of the switch-over: verify that the capacity rules
already declared reproduce the Excel results on at least three clusters of
different technologies. Screen `/clusters/{id}/besoin-offre` for each
cluster to verify — need, installed supply, gap, limiting rule. A
discrepancy with Excel is a mistranscribed rule (variable, expression,
filter) to fix in `/regles` and `/variables`, never a tolerance to accept.
