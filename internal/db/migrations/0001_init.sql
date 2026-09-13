-- Parallax — schéma initial
-- Conventions :
--   dates en TEXT ISO-8601 'YYYY-MM-DD', date_fin NULL = en cours
--   scenario_id NULL = le réel
--   booléens en INTEGER 0/1

PRAGMA foreign_keys = ON;

-- ============================================================ référentiels

CREATE TABLE projet (
    id      INTEGER PRIMARY KEY,
    code    TEXT    NOT NULL UNIQUE,
    libelle TEXT    NOT NULL,
    actif   INTEGER NOT NULL DEFAULT 1 CHECK (actif IN (0, 1))
);

CREATE TABLE environnement (
    id      INTEGER PRIMARY KEY,
    code    TEXT    NOT NULL UNIQUE,
    libelle TEXT    NOT NULL,
    ordre   INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE techno (
    id      INTEGER PRIMARY KEY,
    code    TEXT    NOT NULL UNIQUE,
    libelle TEXT    NOT NULL,
    actif   INTEGER NOT NULL DEFAULT 1 CHECK (actif IN (0, 1))
);

CREATE TABLE tier (
    id      INTEGER PRIMARY KEY,
    code    TEXT    NOT NULL UNIQUE,
    libelle TEXT    NOT NULL,
    ordre   INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE usage_fonctionnel (
    id      INTEGER PRIMARY KEY,
    code    TEXT    NOT NULL UNIQUE,
    libelle TEXT    NOT NULL
);

CREATE TABLE zone (
    id      INTEGER PRIMARY KEY,
    code    TEXT    NOT NULL UNIQUE,
    libelle TEXT    NOT NULL,
    site    TEXT
);

-- ============================================================ utilisateurs

CREATE TABLE utilisateur (
    id      INTEGER PRIMARY KEY,
    login   TEXT    NOT NULL UNIQUE,
    hash    TEXT    NOT NULL,
    nom     TEXT,
    role    TEXT    NOT NULL CHECK (role IN ('ADMIN', 'EDITEUR', 'LECTEUR')),
    actif   INTEGER NOT NULL DEFAULT 1 CHECK (actif IN (0, 1)),
    cree_le TEXT    NOT NULL
);

CREATE TABLE journal (
    id             INTEGER PRIMARY KEY,
    entite         TEXT    NOT NULL,
    entite_id      INTEGER NOT NULL,
    action         TEXT    NOT NULL CHECK (action IN ('CREATION', 'MODIFICATION', 'CORRECTION', 'SUPPRESSION')),
    utilisateur_id INTEGER REFERENCES utilisateur (id),
    horodatage     TEXT    NOT NULL,
    avant          TEXT,
    apres          TEXT
);

CREATE INDEX idx_journal_entite ON journal (entite, entite_id);

-- ============================================================ périmètre

CREATE TABLE cluster (
    id                   INTEGER PRIMARY KEY,
    nom                  TEXT    NOT NULL,
    projet_id            INTEGER NOT NULL REFERENCES projet (id),
    environnement_id     INTEGER NOT NULL REFERENCES environnement (id),
    techno_id            INTEGER NOT NULL REFERENCES techno (id),
    tier_id              INTEGER REFERENCES tier (id),
    usage_fonctionnel_id INTEGER REFERENCES usage_fonctionnel (id),
    commentaire          TEXT,
    actif                INTEGER NOT NULL DEFAULT 1 CHECK (actif IN (0, 1)),
    UNIQUE (projet_id, environnement_id, nom)
);

CREATE INDEX idx_cluster_dims ON cluster (techno_id, tier_id, usage_fonctionnel_id);

-- ============================================================ catalogue

CREATE TABLE modele (
    id                  INTEGER PRIMARY KEY,
    type                TEXT    NOT NULL,
    annee               INTEGER NOT NULL,
    code                TEXT    NOT NULL UNIQUE,
    description         TEXT,
    mode_financement    TEXT CHECK (mode_financement IN ('ACHAT', 'LEASE')),
    duree_lease_mois    INTEGER,
    date_debut_lease    TEXT,
    prix_fournisseur_ht REAL,
    cout_annuel_ht      REAL,
    duree_cout_annees   INTEGER,
    actif               INTEGER NOT NULL DEFAULT 1 CHECK (actif IN (0, 1)),
    UNIQUE (type, annee)
);

-- Une révision est IMMUABLE dès qu'elle est référencée par un serveur_revision.
-- L'application distingue « corriger une erreur » de « créer une révision ».
CREATE TABLE revision (
    id          INTEGER PRIMARY KEY,
    modele_id   INTEGER NOT NULL REFERENCES modele (id),
    numero      INTEGER NOT NULL,
    libelle     TEXT,
    date_effet  TEXT    NOT NULL,
    commentaire TEXT,
    UNIQUE (modele_id, numero)
);

CREATE TABLE composant (
    id                INTEGER PRIMARY KEY,
    revision_id       INTEGER NOT NULL REFERENCES revision (id) ON DELETE CASCADE,
    nature            TEXT    NOT NULL CHECK (nature IN ('CPU', 'RAM', 'DISQUE_DATA', 'GPU', 'NIC', 'AUTRE')),
    code              TEXT    NOT NULL,
    quantite          REAL    NOT NULL,
    capacite_unitaire REAL    NOT NULL,
    unite             TEXT    NOT NULL CHECK (unite IN ('TO', 'GO', 'CORE', 'GBPS', 'POINT', 'TFLOPS', 'TOS', 'UNITE')),
    commentaire       TEXT,
    UNIQUE (revision_id, code)
);

CREATE TABLE modele_noeud (
    id          INTEGER PRIMARY KEY,
    revision_id INTEGER NOT NULL REFERENCES revision (id) ON DELETE CASCADE,
    techno_id   INTEGER NOT NULL REFERENCES techno (id),
    nb_noeuds   INTEGER NOT NULL CHECK (nb_noeuds >= 0),
    UNIQUE (revision_id, techno_id)
);

-- ============================================================ réseau

CREATE TABLE vlan (
    id               INTEGER PRIMARY KEY,
    code             TEXT    NOT NULL,
    projet_id        INTEGER REFERENCES projet (id),
    environnement_id INTEGER REFERENCES environnement (id),
    zone_id    INTEGER REFERENCES zone (id),
    commentaire      TEXT,
    UNIQUE (code)
);

CREATE TABLE plage_ip (
    id       INTEGER PRIMARY KEY,
    vlan_id  INTEGER NOT NULL REFERENCES vlan (id) ON DELETE CASCADE,
    ip_debut TEXT    NOT NULL,
    ip_fin   TEXT    NOT NULL
);

-- ============================================================ scénarios

CREATE TABLE scenario (
    id            INTEGER PRIMARY KEY,
    nom           TEXT    NOT NULL,
    projet_id     INTEGER REFERENCES projet (id),
    description   TEXT    NOT NULL,
    statut        TEXT    NOT NULL CHECK (statut IN ('BROUILLON', 'ACTIF', 'RETENU', 'ABANDONNE')),
    date_creation TEXT    NOT NULL,
    date_cloture  TEXT,
    auteur_id     INTEGER REFERENCES utilisateur (id)
);

-- ============================================================ serveurs

CREATE TABLE serveur (
    id             INTEGER PRIMARY KEY,
    physical_name  TEXT,
    hostname       TEXT,
    serial_number  TEXT,
    zone_id  INTEGER REFERENCES zone (id),
    position_zone    TEXT,
    ip             TEXT,
    vlan_id        INTEGER REFERENCES vlan (id),
    version_os     TEXT,
    typologie TEXT,
    code_appli     TEXT,
    demande_ref  TEXT,
    demande_serveur_ref      TEXT,
    statut         TEXT    NOT NULL CHECK (statut IN ('HYPOTHESE', 'COMMANDE', 'EN_SERVICE', 'DECOMMISSIONNE')),
    scenario_id    INTEGER REFERENCES scenario (id),
    date_entree    TEXT,
    date_sortie    TEXT,
    commentaire    TEXT,
    -- invariant 5 : un serveur HYPOTHESE appartient obligatoirement à un scénario
    CHECK (statut <> 'HYPOTHESE' OR scenario_id IS NOT NULL)
);

CREATE INDEX idx_serveur_scenario ON serveur (scenario_id);
CREATE INDEX idx_serveur_statut   ON serveur (statut);
CREATE INDEX idx_serveur_zone       ON serveur (zone_id);

CREATE TABLE serveur_revision (
    id          INTEGER PRIMARY KEY,
    serveur_id  INTEGER NOT NULL REFERENCES serveur (id) ON DELETE CASCADE,
    revision_id INTEGER NOT NULL REFERENCES revision (id),
    date_debut  TEXT    NOT NULL,
    date_fin    TEXT,
    commentaire TEXT,
    CHECK (date_fin IS NULL OR date_fin >= date_debut)
);

CREATE INDEX idx_serveur_revision ON serveur_revision (serveur_id, date_debut);

CREATE TABLE affectation (
    id          INTEGER PRIMARY KEY,
    serveur_id  INTEGER NOT NULL REFERENCES serveur (id) ON DELETE CASCADE,
    cluster_id  INTEGER NOT NULL REFERENCES cluster (id),
    date_debut  TEXT    NOT NULL,
    date_fin    TEXT,
    scenario_id INTEGER REFERENCES scenario (id),
    commentaire TEXT,
    CHECK (date_fin IS NULL OR date_fin >= date_debut)
);

CREATE INDEX idx_affectation_serveur  ON affectation (serveur_id, date_debut);
CREATE INDEX idx_affectation_cluster  ON affectation (cluster_id, date_debut);
CREATE INDEX idx_affectation_scenario ON affectation (scenario_id);

-- ============================================================ variables

CREATE TABLE variable (
    id          INTEGER PRIMARY KEY,
    code        TEXT    NOT NULL UNIQUE,
    libelle     TEXT    NOT NULL,
    unite       TEXT,
    defaut      REAL,
    commentaire TEXT
);

-- Portée hiérarchique : cluster > tier > techno > environnement > projet > global.
-- Toutes les colonnes de portée sont nullables ; NULL = « ne restreint pas ».
CREATE TABLE variable_valeur (
    id               INTEGER PRIMARY KEY,
    variable_id      INTEGER NOT NULL REFERENCES variable (id) ON DELETE CASCADE,
    scenario_id      INTEGER REFERENCES scenario (id),
    annee            INTEGER NOT NULL,
    projet_id        INTEGER REFERENCES projet (id),
    environnement_id INTEGER REFERENCES environnement (id),
    techno_id        INTEGER REFERENCES techno (id),
    tier_id          INTEGER REFERENCES tier (id),
    cluster_id       INTEGER REFERENCES cluster (id),
    valeur           REAL    NOT NULL,
    commentaire      TEXT,
    modifie_le       TEXT    NOT NULL,
    modifie_par      INTEGER REFERENCES utilisateur (id)
);

CREATE INDEX idx_varval ON variable_valeur (variable_id, annee, scenario_id);

-- Deux valeurs de même variable, même année, même scénario et même portée
-- exacte seraient ambiguës : on l'interdit. COALESCE car SQLite considère
-- deux NULL comme distincts dans un index unique.
CREATE UNIQUE INDEX idx_varval_unique ON variable_valeur (
    variable_id,
    annee,
    COALESCE(scenario_id, -1),
    COALESCE(projet_id, -1),
    COALESCE(environnement_id, -1),
    COALESCE(techno_id, -1),
    COALESCE(tier_id, -1),
    COALESCE(cluster_id, -1)
);

CREATE TABLE variable_valeur_historique (
    id             INTEGER PRIMARY KEY,
    valeur_id      INTEGER NOT NULL,
    ancienne       REAL    NOT NULL,
    nouvelle       REAL    NOT NULL,
    horodatage     TEXT    NOT NULL,
    utilisateur_id INTEGER REFERENCES utilisateur (id)
);

-- ============================================================ règles

CREATE TABLE metrique (
    id      INTEGER PRIMARY KEY,
    code    TEXT NOT NULL UNIQUE,
    libelle TEXT NOT NULL,
    unite   TEXT NOT NULL
);

CREATE TABLE regle (
    id                   INTEGER PRIMARY KEY,
    nom                  TEXT    NOT NULL,
    metrique_id          INTEGER NOT NULL REFERENCES metrique (id),
    expression           TEXT    NOT NULL,
    niveau_evaluation    TEXT    NOT NULL CHECK (niveau_evaluation IN ('PERIMETRE', 'PAR_SERVEUR')),
    composant_offre      TEXT,
    projet_id            INTEGER REFERENCES projet (id),
    environnement_id     INTEGER REFERENCES environnement (id),
    techno_id            INTEGER REFERENCES techno (id),
    tier_id              INTEGER REFERENCES tier (id),
    usage_fonctionnel_id INTEGER REFERENCES usage_fonctionnel (id),
    cluster_id           INTEGER REFERENCES cluster (id),
    actif                INTEGER NOT NULL DEFAULT 1 CHECK (actif IN (0, 1)),
    commentaire          TEXT,
    date_creation        TEXT    NOT NULL
);

CREATE INDEX idx_regle_metrique ON regle (metrique_id, actif);

-- ============================================================ contraintes

CREATE TABLE contrainte (
    id          INTEGER PRIMARY KEY,
    cluster_id  INTEGER NOT NULL REFERENCES cluster (id) ON DELETE CASCADE,
    scenario_id INTEGER REFERENCES scenario (id),
    annee       INTEGER NOT NULL,
    type        TEXT    NOT NULL CHECK (type IN (
                    'MIN_TOTAL', 'MIN_PAR_ZONE', 'MULTIPLE_TOTAL',
                    'MULTIPLE_PAR_ZONE', 'NB_ZONES', 'EQUILIBRAGE_ZONE')),
    valeur      REAL,
    portee      TEXT CHECK (portee IS NULL OR portee IN ('NOUVEAUX', 'TOUS')),
    commentaire TEXT
);

CREATE UNIQUE INDEX idx_contrainte_unique ON contrainte (
    cluster_id, annee, type, COALESCE(scenario_id, -1)
);

-- ============================================================ vues

CREATE TABLE vue (
    id            INTEGER PRIMARY KEY,
    nom           TEXT    NOT NULL,
    proprietaire  INTEGER REFERENCES utilisateur (id),
    partagee      INTEGER NOT NULL DEFAULT 0 CHECK (partagee IN (0, 1)),
    axes          TEXT    NOT NULL,
    filtres       TEXT    NOT NULL,
    colonnes      TEXT    NOT NULL,
    date_creation TEXT    NOT NULL
);
