-- Parallax — revue de performance (2026-09-12) : index manquants
--
-- Mesuré sur une base synthétique à dix ans d'usage (5 000 serveurs,
-- 15 000 affectations, 60 000 lignes de journal — internal/web/perf_test.go,
-- PARALLAX_PERF=1) : les plans de requête ci-dessous parcouraient une table
-- entière. Aucun n'était lent à ce volume, mais chacun grandit avec les
-- années — le journal surtout.

-- clés de rapprochement de la mise à jour en masse et de l'adressage ; les
-- index d'unicité partiels (0006) ne servent pas aux recherches par valeur
CREATE INDEX idx_serveur_hostname      ON serveur (hostname);
CREATE INDEX idx_serveur_physical_name ON serveur (physical_name);
CREATE INDEX idx_serveur_demande       ON serveur (demande_ref, demande_serveur_ref);
CREATE INDEX idx_serveur_vlan          ON serveur (vlan_id);

-- journal : filtre par auteur, purge et filtres par période
CREATE INDEX idx_journal_utilisateur ON journal (utilisateur_id);
CREATE INDEX idx_journal_horodatage  ON journal (horodatage);

-- accès par clé étrangère sans index jusqu'ici
CREATE INDEX idx_plage_ip_vlan            ON plage_ip (vlan_id);
CREATE INDEX idx_varval_historique_valeur ON variable_valeur_historique (valeur_id);
CREATE INDEX idx_vue_proprietaire         ON vue (proprietaire);
