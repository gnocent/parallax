-- Parallax — v3 : demandes de matériel, licences, adressage IP
-- (docs/backlog.md v3.1 à v3.3, décisions du 2026-09-12).
--
-- Aucune modification de 0001 : première migration additive depuis la mise
-- au propre du schéma initial — c'est la règle à partir d'ici.

-- ------------------------------------------------------------ paramètres
-- Réglages globaux de l'application, une ligne par clé. Première clé :
-- gabarit_demande (v3.2), le texte à variables {nom} rendu pour chaque
-- serveur dans l'écran des demandes.
CREATE TABLE parametre (
    cle         TEXT    PRIMARY KEY,
    valeur      TEXT    NOT NULL,
    modifie_le  TEXT    NOT NULL,
    modifie_par INTEGER REFERENCES utilisateur (id)
);

-- ------------------------------------------------------------ licences (v3.3)
-- Un contrat par techno et par année, surchargeable par scénario ; résolu à
-- l'année de la date de la vue, à défaut le plus récent des antérieurs.
-- Le niveau n'a pas d'objet pour NOEUDS mais reste obligatoire pour garder
-- une ligne homogène ; ram_max_go est requis sauf NOEUDS (contrôlé en code,
-- un CHECK conditionnel serait moins lisible que le message d'erreur).
CREATE TABLE licence_contrat (
    id               INTEGER PRIMARY KEY,
    techno_id        INTEGER NOT NULL REFERENCES techno (id),
    annee            INTEGER NOT NULL,
    scenario_id      INTEGER REFERENCES scenario (id),
    mecanisme        TEXT    NOT NULL CHECK (mecanisme IN ('NOEUDS', 'MAX_NOEUDS_RAM', 'RAM')),
    niveau           TEXT    NOT NULL CHECK (niveau IN ('MACHINE', 'CLUSTER', 'GLOBAL')),
    ram_max_go       REAL    CHECK (ram_max_go IS NULL OR ram_max_go > 0),
    cout_unitaire_ht REAL    CHECK (cout_unitaire_ht IS NULL OR cout_unitaire_ht >= 0),
    commentaire      TEXT
);

CREATE UNIQUE INDEX idx_licence_contrat_unique
    ON licence_contrat (techno_id, annee, COALESCE(scenario_id, -1));

-- ------------------------------------------------------------ adressage IP (v3.1)
-- Critère d'application supplémentaire (cluster) et curseur d'attribution :
-- la dernière adresse proposée dans ce VLAN, pour reprendre après elle et ne
-- revenir sur une adresse libérée qu'au tour suivant.
ALTER TABLE vlan ADD COLUMN cluster_id  INTEGER REFERENCES cluster (id);
ALTER TABLE vlan ADD COLUMN derniere_ip TEXT;

-- Les incohérences d'adressage (adresse portée par plusieurs serveurs, hors
-- plage, VLAN incompatible) ne sont pas des contraintes : elles se calculent
-- à la lecture et se signalent sans bloquer (backlog v3.1).
