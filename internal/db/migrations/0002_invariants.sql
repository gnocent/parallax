-- Parallax — invariants posés en base
--
-- Complète le §12 de docs/modele-donnees.md : ce que SQLite peut garantir par
-- index l'est ici ; le reste (non-chevauchement de périodes fermées,
-- immuabilité des révisions, non-recouvrement des règles) est vérifié dans
-- internal/depot avant écriture.

-- Invariant 3 — un serveur n'a qu'UNE affectation active à la fois dans un
-- scénario donné. « Active » = date_fin IS NULL. Le réel et chaque scénario
-- comptent séparément ; COALESCE ramène le réel (NULL) dans un même seau.
CREATE UNIQUE INDEX idx_affectation_active_unique
    ON affectation (serveur_id, COALESCE(scenario_id, -1))
    WHERE date_fin IS NULL;

-- Invariant 4 — au plus un rattachement de révision ouvert par serveur. Le
-- non-chevauchement des périodes déjà fermées est contrôlé côté dépôt.
CREATE UNIQUE INDEX idx_serveur_revision_ouverte_unique
    ON serveur_revision (serveur_id)
    WHERE date_fin IS NULL;
