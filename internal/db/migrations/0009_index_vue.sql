-- Parallax — index manquant sur vue.partagee (2026-10-23)
--
-- La liste des vues d'un utilisateur (depot/vue.go) filtre par
-- « partagee = 1 OR proprietaire = ? ». SQLite ne décompose un OR en deux
-- recherches indexées que si chaque terme a son index : avec le seul
-- idx_vue_proprietaire (0007), le plan restait un parcours de table entière,
-- signalé « !! » par le test de charge (internal/web/perf_test.go).
CREATE INDEX idx_vue_partagee ON vue (partagee);
