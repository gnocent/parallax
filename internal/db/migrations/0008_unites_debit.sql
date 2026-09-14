-- Parallax — unités de débit pour les composants (2026-09-13)
--
-- Élargit le vocabulaire d'unité du composant, jusqu'ici TO|GO|CORE|GBPS|
-- POINT|TFLOPS|TOS|UNITE, avec MOS (Mo/s) et GOS (Go/s) — pour qualifier par
-- exemple le débit séquentiel d'un disque comme composant AUTRE (code libre,
-- docs/modele-donnees.md §4.2), à côté de sa capacité (hdd/ssd, toujours en
-- To). Pas de nouveau composant canonique : ces deux unités s'ajoutent
-- simplement à celles déjà disponibles pour n'importe quel composant.
--
-- SQLite ne permet pas de modifier une contrainte CHECK en place : la table
-- est reconstruite à l'identique hormis cette contrainte. Rien ne référence
-- composant.id par clé étrangère (recherché dans toutes les migrations), la
-- reconstruction n'a donc pas de ligne dépendante à reporter.

CREATE TABLE composant_nouveau (
    id                INTEGER PRIMARY KEY,
    revision_id       INTEGER NOT NULL REFERENCES revision (id) ON DELETE CASCADE,
    nature            TEXT    NOT NULL CHECK (nature IN ('CPU', 'RAM', 'DISQUE_DATA', 'GPU', 'NIC', 'AUTRE')),
    code              TEXT    NOT NULL,
    quantite          REAL    NOT NULL,
    capacite_unitaire REAL    NOT NULL,
    unite             TEXT    NOT NULL CHECK (unite IN ('TO', 'GO', 'CORE', 'GBPS', 'POINT', 'TFLOPS', 'TOS', 'GOS', 'MOS', 'UNITE')),
    commentaire       TEXT,
    UNIQUE (revision_id, code)
);

INSERT INTO composant_nouveau SELECT * FROM composant;

DROP TABLE composant;

ALTER TABLE composant_nouveau RENAME TO composant;
