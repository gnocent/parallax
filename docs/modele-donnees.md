# Parallax — Modèle de données

*Français · [English](modele-donnees.en.md)*

> Référence technique pour les personnes qui développent sur Parallax.
> Pour une présentation fonctionnelle de l'application, voir le
> [guide utilisateur](guide-utilisateur.md).

Cible : SQLite, mode WAL, accès concurrent de 3-4 rédacteurs et 10-15 lecteurs.
Volumétrie : ~2000 serveurs, quelques centaines de modèles et de règles. Aucun enjeu
de performance ; l'enjeu est la justesse du modèle.

Conventions : identifiants techniques entiers, dates en `TEXT` ISO-8601 (`YYYY-MM-DD`),
`date_fin` nulle signifiant « toujours en cours », `scenario_id` nul signifiant « le réel ».

---

# 1. Vue d'ensemble

```
                    ┌──────────────┐
                    │   scenario   │  (NULL = réel)
                    └──────┬───────┘
                           │ surcharge
       ┌───────────────────┼────────────────────┐
       │                   │                    │
┌──────▼──────┐    ┌───────▼────────┐   ┌───────▼────────┐
│ affectation │    │ variable_valeur│   │   contrainte   │
└──┬───────┬──┘    └───────┬────────┘   └───────┬────────┘
   │       │               │                    │
   │  ┌────▼─────┐         │                    │
   │  │ cluster  │◄────────┴────────────────────┘
   │  └────┬─────┘   (portée de la valeur / contrainte)
   │       │
   │       │ filtré par
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

# 2. Référentiels

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
  ordre       INTEGER NOT NULL           -- pour l'affichage
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

# 3. Périmètre : le cluster

Entité centrale. Un cluster est l'intersection concrète des dimensions : c'est à lui
qu'on affecte des serveurs et c'est lui que les règles ciblent.

```sql
CREATE TABLE cluster (
  id                    INTEGER PRIMARY KEY,
  nom                   TEXT NOT NULL,          -- ElasticCold1
  projet_id             INTEGER NOT NULL REFERENCES projet(id),
  environnement_id      INTEGER NOT NULL REFERENCES environnement(id),
  techno_id             INTEGER NOT NULL REFERENCES techno(id),
  tier_id               INTEGER REFERENCES tier(id),               -- NULL possible
  usage_fonctionnel_id  INTEGER REFERENCES usage_fonctionnel(id),  -- NULL possible
  commentaire           TEXT,
  actif                 INTEGER NOT NULL DEFAULT 1,
  UNIQUE (projet_id, environnement_id, nom)
);
```

> **Note.** Le tier et l'usage sont facultatifs : Kafka et Nomad n'ont pas de tier ;
> l'usage ne distingue que les cas où une même techno sert plusieurs domaines
> fonctionnels dans un même projet.

---

# 4. Catalogue matériel

## 4.1 Modèle et révisions

```sql
CREATE TABLE modele (
  id                  INTEGER PRIMARY KEY,
  type                TEXT NOT NULL,            -- DENSE
  annee               INTEGER NOT NULL,         -- 2025
  code                TEXT NOT NULL UNIQUE,     -- DENSE-2025 (clé de jointure historique)
  description         TEXT,
  mode_financement    TEXT,                     -- ACHAT | LEASE
  duree_lease_mois    INTEGER,
  date_debut_lease    TEXT,
  prix_fournisseur_ht REAL,                     -- devis constructeur, cadrage amont
  cout_annuel_ht      REAL,                     -- devis infra, constant sur la durée
  duree_cout_annees   INTEGER,                  -- 5 ou 6
  actif               INTEGER NOT NULL DEFAULT 1
);

-- Une révision est IMMUABLE une fois créée et référencée.
-- Toute évolution matérielle réelle crée une nouvelle révision.
-- La correction d'erreur est un acte distinct dans l'IHM, avec avertissement.
CREATE TABLE revision (
  id            INTEGER PRIMARY KEY,
  modele_id     INTEGER NOT NULL REFERENCES modele(id),
  numero        INTEGER NOT NULL,               -- 1, 2, 3...
  libelle       TEXT,                           -- "ajout 8 SSD 3,84 To"
  date_effet    TEXT NOT NULL,
  commentaire   TEXT,
  UNIQUE (modele_id, numero)
);
```

## 4.2 Composants

Remplace les colonnes CPU / RAM / DISK_70 précalculées. Le calcul est reporté dans les
formules, ce qui préserve la capacité brute et rend les coefficients ajustables.

```sql
CREATE TABLE composant (
  id                 INTEGER PRIMARY KEY,
  revision_id        INTEGER NOT NULL REFERENCES revision(id),
  nature             TEXT NOT NULL,   -- CPU | RAM | DISQUE_DATA | GPU | NIC | AUTRE
  code               TEXT NOT NULL,   -- variable exposée aux formules : ssd, cpu, ram
  quantite           REAL NOT NULL,   -- 24 disques, 2 cartes, 4 GPU ; 1 pour un scalaire
  capacite_unitaire  REAL NOT NULL,   -- 24 (To), 25 (Gbps) ; 64 cœurs, 1024 Go pour un scalaire
  unite              TEXT NOT NULL,   -- TO | GO | CORE | GBPS | POINT | TFLOPS | TOS | GOS | MOS | UNITE
  commentaire        TEXT,
  UNIQUE (revision_id, code)
);
```

**Vocabulaire canonique.** Le code est libre en base, mais l'import des
modèles, le constructeur de vues et la comparaison de scénarios ne
connaissent que les codes ci-dessous (`depot.ComposantsCanoniques`). Un code
hors liste (nature AUTRE) reste utilisable dans les formules, pas dans les
colonnes de vues. Deux formes :

- **scalaire** : quantité 1, la capacité unitaire porte le total du serveur.
  On ne décompose pas en sockets ni en barrettes — seul le total sert au
  calcul, et les scores de puissance CPU n'ont pas d'unité physique ;
- **dénombré** : quantité × capacité unitaire, les éléments sont supposés
  identiques (cartes réseau, disques d'une même famille, GPU).

| code | nature | forme | quantité | capacité unitaire | unité |
|---|---|---|---|---|---|
| `cpu` | CPU | scalaire | 1 | nombre total de cœurs | CORE |
| `specrate` | CPU | scalaire | 1 | score SPECrate base | POINT |
| `phoronix` | CPU | scalaire | 1 | score Phoronix Test Suite | POINT |
| `ram` | RAM | scalaire | 1 | Go au total | GO |
| `hdd` | DISQUE_DATA | dénombré | nombre de disques | To par disque | TO |
| `ssd` | DISQUE_DATA | dénombré | nombre de disques | To par disque | TO |
| `nic` | NIC | dénombré | nombre de cartes | Gbps par carte | GBPS |
| `gpu` | GPU | dénombré | nombre de GPU | 1 | UNITE |
| `gpu_ram` | GPU | dénombré | nombre de GPU | Go par GPU | GO |
| `gpu_fp8` | GPU | dénombré | nombre de GPU | TFLOPS FP8 par GPU | TFLOPS |
| `gpu_bp` | GPU | dénombré | nombre de GPU | bande passante mémoire par GPU (To/s) | TOS |

Les trois attributs GPU répètent la quantité du composant `gpu` : c'est la
seule redondance admise, parce que chaque attribut doit rester une variable
distincte des formules (`gpu_ram_total`, `gpu_fp8_machine`…). Les disques
système ne sont pas décrits : ils n'entrent dans aucun calcul de capacité.

GOS (Go/s) et MOS (Mo/s, migration `0008`) ne sont les unités d'aucun code
canonique — un débit de disque, par exemple, se déclare en composant `AUTRE`
(code libre, ex. `hdd_debit`) : visible des formules, pas des colonnes de
vues ni de l'import, au même titre que tout code hors de ce vocabulaire.

Exemple pour un serveur de stockage :

| nature | code | quantite | capacite_unitaire | unite |
|---|---|---|---|---|
| CPU | `cpu` | 1 | 64 | CORE |
| CPU | `specrate` | 1 | 480 | POINT |
| RAM | `ram` | 1 | 1024 | GO |
| DISQUE_DATA | `hdd` | 24 | 24 | TO |
| DISQUE_DATA | `ssd` | 2 | 3,84 | TO |
| NIC | `nic` | 2 | 25 | GBPS |

Les formules disposent de `hdd_total = 576`, et le calcul historique
`24 × 24 × 0,9 × 0,7 = 362,88` s'écrit
`hdd_total * coef_formatage * taux_remplissage_max`.

Ce vocabulaire a remplacé le 2026-09-11 la décomposition initiale (sockets ×
cœurs, barrettes × Go, disques système). La migration `0001` a été modifiée
en place pour les énumérations de `nature` et `unite` — exception assumée à
la règle « jamais modifiée après application » : aucune instance déployée,
les bases repartent vides.

## 4.3 Nœuds installables par technologie

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

# 5. Serveurs

```sql
CREATE TABLE serveur (
  id                INTEGER PRIMARY KEY,     -- identité technique, seule clé stable
  physical_name     TEXT,                    -- attribut, PAS une identité
  hostname          TEXT,
  serial_number     TEXT,
  zone_id     INTEGER REFERENCES zone(id),
  position_zone       TEXT,                    -- documentaire
  ip                TEXT,                    -- IP de production uniquement
  vlan_id           INTEGER REFERENCES vlan(id),
  version_os        TEXT,
  typologie    TEXT,                    -- P | A | D | PA | AD | PAD, saisi, attendu par l'outil de demande
  code_appli        TEXT,
  demande_ref     TEXT,
  demande_serveur_ref         TEXT,
  statut            TEXT NOT NULL,           -- HYPOTHESE|COMMANDE|EN_SERVICE|DECOMMISSIONNE
  scenario_id       INTEGER REFERENCES scenario(id),  -- NULL = réel
  date_entree       TEXT,
  date_sortie       TEXT,
  commentaire       TEXT
);

CREATE INDEX idx_serveur_scenario ON serveur(scenario_id);
CREATE INDEX idx_serveur_statut   ON serveur(statut);
```

> **Serveurs hypothétiques.** Un serveur créé dans un scénario porte `scenario_id`.
> La promotion d'un scénario consiste à passer `scenario_id` à NULL et le statut à
> `COMMANDE`. Aucune donnée n'est détruite.

## 5.1 Rattachement au modèle, avec révision et dates

```sql
CREATE TABLE serveur_revision (
  id           INTEGER PRIMARY KEY,
  serveur_id   INTEGER NOT NULL REFERENCES serveur(id),
  revision_id  INTEGER NOT NULL REFERENCES revision(id),
  date_debut   TEXT NOT NULL,
  date_fin     TEXT,                          -- NULL = en cours
  commentaire  TEXT
);

CREATE INDEX idx_serveur_revision ON serveur_revision(serveur_id, date_debut);
```

Permet de connaître les caractéristiques matérielles d'un serveur **à n'importe quelle
date passée**, sans bitemporalité, grâce à l'immuabilité des révisions.

## 5.2 Affectation

```sql
CREATE TABLE affectation (
  id           INTEGER PRIMARY KEY,
  serveur_id   INTEGER NOT NULL REFERENCES serveur(id),
  cluster_id   INTEGER NOT NULL REFERENCES cluster(id),
  date_debut   TEXT NOT NULL,
  date_fin     TEXT,                          -- NULL = en cours
  scenario_id  INTEGER REFERENCES scenario(id),  -- NULL = réel
  commentaire  TEXT
);

CREATE INDEX idx_affectation_serveur  ON affectation(serveur_id, date_debut);
CREATE INDEX idx_affectation_cluster  ON affectation(cluster_id, date_debut);
CREATE INDEX idx_affectation_scenario ON affectation(scenario_id);
```

Un serveur sans affectation active est candidat naturel à la réutilisation.
Une réaffectation entre projets est simplement une fin d'affectation suivie d'une
nouvelle — y compris à l'intérieur d'un scénario.

## 5.3 Réseau

```sql
CREATE TABLE vlan (
  id                INTEGER PRIMARY KEY,
  code              TEXT NOT NULL UNIQUE,
  projet_id         INTEGER REFERENCES projet(id),          -- critères d'application :
  environnement_id  INTEGER REFERENCES environnement(id),   -- NULL = tous
  zone_id           INTEGER REFERENCES zone(id),
  cluster_id        INTEGER REFERENCES cluster(id),         -- v3.1
  derniere_ip       TEXT,                                   -- curseur d'attribution (v3.1)
  commentaire       TEXT
);

CREATE TABLE plage_ip (
  id        INTEGER PRIMARY KEY,
  vlan_id   INTEGER NOT NULL REFERENCES vlan(id),
  ip_debut  TEXT NOT NULL,   -- intervalles fermés ; passerelle et réservées hors intervalle
  ip_fin    TEXT NOT NULL
);
```

Adresses en IPv4 texte, comparées numériquement. Le pool est global : une
adresse est prise dès qu'un serveur non décommissionné la porte, réel ou
hypothétique d'un scénario non abandonné — l'attribution n'a lieu qu'à la
concrétisation d'une hypothèse (demande de devis), jamais à sa création,
donc sans surcharge par scénario. Proposition : première adresse libre
après `derniere_ip` dans les plages du VLAN, en rebouclant ; l'utilisateur
peut forcer toute adresse. Les incohérences (adresse multiple, hors plage,
VLAN incompatible avec les critères) ne sont pas des contraintes de base :
elles se calculent à la lecture et se signalent (v3.1).

---

# 6. Scénarios

```sql
CREATE TABLE scenario (
  id             INTEGER PRIMARY KEY,
  nom            TEXT NOT NULL,
  projet_id      INTEGER REFERENCES projet(id),   -- rattachement, pas restriction
  description    TEXT NOT NULL,                   -- obligatoire : on y revient des mois après
  statut         TEXT NOT NULL,                   -- BROUILLON|ACTIF|RETENU|ABANDONNE
  date_creation  TEXT NOT NULL,
  date_cloture   TEXT,
  auteur_id      INTEGER REFERENCES utilisateur(id)
);
```

Pas de dérivation entre scénarios : une seule génération de calques. Le rattachement à un
projet sert au classement ; les deltas peuvent porter sur des serveurs de tout projet,
pour permettre les hypothèses de libération et réaffectation entre projets.

**Résolution des affectations sous un scénario.** Le seau (`COALESCE(scenario_id, -1)`)
garantit l'unicité de l'affectation active *dans chaque seau pris isolément* (invariant 3),
mais ne dit rien de ce qu'un écran doit montrer quand un même serveur a une affectation
active dans le réel *et* une autre dans le scénario. La règle, symétrique de celle des
variables et des contraintes (§7.2, §8) — surcharge, jamais fusion — est : **pour un
serveur donné, s'il existe au moins une ligne d'affectation dans le seau du scénario, cette
ligne (ou son absence de ligne active à la date considérée) l'emporte *entièrement* sur le
réel pour ce serveur.** Concrètement :

- **déplacer** un serveur réel dans un scénario : une affectation vers le nouveau cluster,
  ouverte dans le seau du scénario. Le réel n'est pas touché ; sous le scénario, le serveur
  n'apparaît plus dans son cluster réel.
- **retirer** un serveur réel dans un scénario, sans le réaffecter ailleurs : ouvrir puis
  refermer une affectation dans le seau du scénario (mêmes gestes `Affecter`/`Desaffecter`
  qu'en réel). Le scénario porte alors une ligne pour ce serveur qui n'est active à aucune
  date — ce qui suffit à faire gagner la surcharge, sans qu'il faille de représentation
  séparée d'un « retrait » ni de `cluster_id` nullable.
- **ajouter** un serveur hypothétique : aucune ligne réelle à surcharger, la ligne du
  scénario s'applique directement.

`internal/depot.AffectationsResolues` implémente cette résolution ; `ListerAffectationsCluster`
reste la lecture brute (réel plus scénario, sans arbitrage), utile pour lister les deltas
eux-mêmes plutôt que l'état résultant.

**Promotion.** Elle applique au réel exactement ce que la lecture montrait — pas un simple
passage de `scenario_id` à NULL, qui laisserait un serveur déplacé avec deux affectations
ouvertes (refusé par `idx_affectation_active_unique`) et une variable surchargée avec deux
valeurs réelles à la même portée. Pour chaque serveur touché : l'affectation réelle courante
est close la veille du déplacement, ou à la date du retrait (la ligne clone du retrait,
artefact technique, est fusionnée dans la ligne réelle qu'elle recopiait) ; les lignes du
scénario rejoignent ensuite le réel, après contrôle qu'elles ne recouvrent aucune vie réelle.
Un déplacement daté au plus tard le début de l'affectation réelle courante est refusé
(`ErrChevauchement`, en nommant le serveur) : il faudrait réécrire l'histoire. Une surcharge
de variable ou de contrainte à une portée où le réel a déjà une valeur l'écrase (historisée
pour les variables) au lieu de la doubler. Voir `depot.Promouvoir`.

---

# 7. Variables

## 7.1 Déclaration

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

## 7.2 Valeurs, résolues par (scénario, année) et portée

```sql
CREATE TABLE variable_valeur (
  id                    INTEGER PRIMARY KEY,
  variable_id           INTEGER NOT NULL REFERENCES variable(id),
  scenario_id           INTEGER REFERENCES scenario(id),   -- NULL = réel
  annee                 INTEGER NOT NULL,
  -- portée : du plus général au plus spécifique, toutes colonnes nullables
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

-- Historique de sécurité contre l'écrasement accidentel
CREATE TABLE variable_valeur_historique (
  id            INTEGER PRIMARY KEY,
  valeur_id     INTEGER NOT NULL,
  ancienne      REAL NOT NULL,
  nouvelle      REAL NOT NULL,
  horodatage    TEXT NOT NULL,
  utilisateur_id INTEGER REFERENCES utilisateur(id)
);
```

**Règle de résolution** — pour un (cluster, année, scénario) donné :

1. chercher dans le scénario, sinon dans le réel ;
2. à égalité de scénario, retenir la valeur dont la portée est la **plus spécifique**
   parmi celles qui s'appliquent (cluster > tier > techno > environnement > projet > global) ;
3. à défaut, la valeur par défaut de la variable.

C'est cette résolution qui permet de saisir le débit journalier une seule fois au niveau
projet / environnement tout en spécialisant la rétention par tier.

---

# 8. Règles de capacité

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
  expression          TEXT NOT NULL,     -- évaluée par le moteur d'expressions
  niveau_evaluation   TEXT NOT NULL,     -- PERIMETRE | PAR_SERVEUR
  composant_offre     TEXT,              -- code du composant fournissant la métrique
  -- filtre définissant le domaine d'application, colonnes nullables
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

**Invariant à faire respecter par l'application** : deux règles actives portant sur la
même métrique ne doivent pas avoir de filtres qui se chevauchent, c'est-à-dire matcher un
même cluster. Vérification à l'enregistrement, refus explicite en cas de conflit avec
désignation de la règle concurrente. Cela supprime toute règle de priorité implicite.

Les règles sont **dupliquables** et **désactivables** — c'est ainsi qu'on fait évoluer
une règle d'une année sur l'autre sans perdre la précédente.

## 8.1 Niveaux d'évaluation

| Niveau | Sémantique | Exemple |
|---|---|---|
| `PERIMETRE` | la formule est évaluée une fois sur les agrégats du cluster | `ceil(ram_totale / ram_max_licence)` |
| `PAR_SERVEUR` | la formule est évaluée par serveur puis sommée | `sum(ceil(ram_machine / ram_max_licence))` |

Les deux sont nécessaires : les deux mécanismes de licence rencontrés en exigent chacun un.

## 8.2 Licences (v3.3)

Les licences ne sont pas une règle par cluster : le contrat se compte sur
un périmètre choisi dans une vue, et certains niveaux ne sont pas additifs.
Un contrat par techno et par année, surchargeable par scénario.

```sql
CREATE TABLE licence_contrat (
  id               INTEGER PRIMARY KEY,
  techno_id        INTEGER NOT NULL REFERENCES techno(id),
  annee            INTEGER NOT NULL,
  scenario_id      INTEGER REFERENCES scenario(id),   -- NULL = réel
  mecanisme        TEXT NOT NULL,    -- NOEUDS | MAX_NOEUDS_RAM | RAM
  niveau           TEXT NOT NULL,    -- MACHINE | CLUSTER | GLOBAL (sans objet pour NOEUDS)
  ram_max_go       REAL,             -- requis sauf NOEUDS
  cout_unitaire_ht REAL,
  commentaire      TEXT,
  UNIQUE (techno_id, annee, COALESCE(scenario_id, -1))
);
```

Résolution pour une vue à une date : le contrat de l'année de la date, à
défaut le plus récent des années antérieures (un contrat court jusqu'au
suivant). Calcul sur un groupe de la vue, par techno :

| mécanisme | MACHINE | CLUSTER | GLOBAL |
|---|---|---|---|
| `NOEUDS` | Σ nb_noeuds | idem | idem |
| `MAX_NOEUDS_RAM` | Σ max(nb_noeuds, ceil(ram / ram_max)) par serveur | Σ max(Σ nb_noeuds, ceil(Σ ram / ram_max)) par cluster | max(Σ nb_noeuds, ceil(Σ ram / ram_max)) |
| `RAM` | Σ ceil(ram / ram_max) par serveur | Σ ceil(Σ ram / ram_max) par cluster | ceil(Σ ram / ram_max) |

`nb_noeuds` : `modele_noeud` de la révision du serveur pour la techno du
cluster ; `ram` : composant `ram`. Coût = unités × coût unitaire.

---

# 9. Contraintes de dimensionnement

Portées par le cluster, et indexées par (scénario, année) comme les variables — le nombre
de zones d'un cluster peut changer d'une année sur l'autre, et c'est précisément le
genre d'hypothèse à comparer.

```sql
CREATE TABLE contrainte (
  id           INTEGER PRIMARY KEY,
  cluster_id   INTEGER NOT NULL REFERENCES cluster(id),
  scenario_id  INTEGER REFERENCES scenario(id),   -- NULL = réel
  annee        INTEGER NOT NULL,
  type         TEXT NOT NULL,
  -- MIN_TOTAL | MIN_PAR_ZONE | MULTIPLE_TOTAL | MULTIPLE_PAR_ZONE
  -- | NB_ZONES | EQUILIBRAGE_ZONE
  valeur       REAL,
  portee       TEXT,                              -- NOUVEAUX | TOUS (pour EQUILIBRAGE_ZONE)
  commentaire  TEXT
);
```

## 9.1 Algorithme de dimensionnement

Pour un cluster, une année, un scénario et un modèle candidat :

```
1. règles ← règles actives dont le filtre matche le cluster
2. pour chaque règle r :
     besoin[r]   = eval(r.expression, variables résolues)
     offre_unit  = valeur du composant r.composant_offre dans le modèle candidat
     nb_brut[r]  = besoin[r] / offre_unit
3. nb = max(nb_brut)                     ← la règle gagnante est le facteur limitant
4. appliquer les contraintes du cluster, dans l'ordre :
     ceil, MIN_TOTAL, MULTIPLE_TOTAL, puis répartition et contraintes par zone
5. retourner nb, la règle limitante, le coût, la capacité obtenue et le surplus
```

L'ordre importe : arrondir chaque règle avant le `max` donnerait un résultat différent.

Répété sur plusieurs modèles candidats, cet algorithme produit le tableau de comparaison
qui sert à trouver l'équilibre technico-économique.

---

# 10. Vues sauvegardées

```sql
CREATE TABLE vue (
  id           INTEGER PRIMARY KEY,
  nom          TEXT NOT NULL,
  proprietaire INTEGER REFERENCES utilisateur(id),
  partagee     INTEGER NOT NULL DEFAULT 0,
  axes         TEXT NOT NULL,   -- JSON : axes de regroupement ordonnés
  filtres      TEXT NOT NULL,   -- JSON
  colonnes     TEXT NOT NULL,   -- JSON : agrégats affichés
  date_creation TEXT NOT NULL
);
```

Équivalent des tableaux croisés dynamiques actuels. Toute vue est exportable en Excel.
Une vue se lit à une date (v3.0, défaut aujourd'hui) : affectations et
révisions résolues à cette date. Les colonnes de licences (§8.2) se
calculent sur le groupe, pas par somme de lignes ; quand elles sont
demandées sans axe techno, le tableau se découpe par techno.

## 10.1 Paramètres d'application

```sql
CREATE TABLE parametre (
  cle         TEXT PRIMARY KEY,   -- gabarit_demande
  valeur      TEXT NOT NULL,
  modifie_le  TEXT NOT NULL,
  modifie_par INTEGER REFERENCES utilisateur(id)
);
```

`gabarit_demande` : texte libre avec retours à la ligne et variables
`{nom}` (`{{` pour une accolade littérale), rendu pour chaque serveur —
catalogue des variables affiché sur l'écran (`/parametres/gabarit`) et
décrit dans le [guide utilisateur](guide-utilisateur.md#13-demandes-de-matériel).
Un seul gabarit pour toute l'application, édité par un administrateur ;
variable inconnue refusée à la saisie, valeur absente rendue vide.

`sauvegarde_dossier`, `sauvegarde_heure` (`HH:MM`), `sauvegarde_retention_jours` :
réglages de la sauvegarde locale automatique (backlog v3.7,
`/parametres/sauvegarde`, `docs/deploiement.md` §6.1 bis) ; vides ou absents,
la fonctionnalité reste désactivée. `sauvegarde_derniere_reussite` (RFC 3339) et `sauvegarde_derniere_verification`
(`YYYY-MM-DD`) sont un état interne écrit par le planificateur
(`cmd/parallax`), jamais saisis à l'écran. `depot.ActiviteDepuis` exclut
toute l'entité `parametre` du journal (pas seulement ces deux clés) : les
compter créerait une boucle où chaque vérification s'auto-déclarerait
changée.

---

# 11. Utilisateurs et traçabilité

La table `journal` (ci-dessous) est alimentée à chaque écriture : une
ligne par entité modifiée, `avant` et `apres` en JSON de la ligne entière,
écrite dans la transaction de la modification. Aucune restauration
automatique depuis le journal — voir le
[guide utilisateur](guide-utilisateur.md#16-journal-des-modifications) pour
la justification. Purge au-delà de 800 jours.

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

Journal volontairement léger, sans ambition réglementaire. Il sert surtout à comprendre
qui a changé quoi lorsqu'un chiffre surprend.

---

# 12. Invariants à faire respecter

1. Une **révision référencée par un `serveur_revision` est immuable** : toute évolution
   crée une nouvelle révision. La correction d'erreur est une action distincte de l'IHM,
   assortie d'un avertissement.
2. **Aucun chevauchement de règles actives** sur une même métrique.
3. Un serveur n'a **qu'une affectation active** à la fois dans un scénario donné.
4. Les périodes `serveur_revision` d'un même serveur **ne se chevauchent pas**.
5. Un serveur de statut `HYPOTHESE` porte obligatoirement un `scenario_id`.
6. La promotion d'un scénario ne supprime rien : elle bascule `scenario_id` à NULL et
   clôt les scénarios concurrents en `ABANDONNE`.
7. Une adresse IP n'est proposée que si elle n'est consommée ni dans le réel, ni dans le
   scénario courant.

---

# 13. Ce que remplace ce schéma dans un tableur d'inventaire

| Colonne typique d'un tableur | Devient |
|---|---|
| projet, environnement, usage | dimensions du `cluster` (`projet_id`, `environnement_id`, `techno_id` / `tier_id` / `usage_fonctionnel_id`) |
| type de matériel et année | `modele.code`, via `serveur_revision` |
| fin de lease | dérivée de `modele.date_debut_lease` + `duree_lease_mois` |
| CPU, RAM, disques, réseau, GPU, capacité « utile » précalculée | dérivés des `composant` de la révision + variables |
| nombre de nœuds | `modele_noeud` (par techno) |
| prix fournisseur, coût annuel | `modele.prix_fournisseur_ht`, `modele.cout_annuel_ht` |
| commentaire à coller dans l'outil de demande | généré à la demande (v3.2) |
| cible de réutilisation, confirmation de décommissionnement | remplacés par `affectation` datée + `serveur.statut` |
| description matérielle | `modele.description` |
