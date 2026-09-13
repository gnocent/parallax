-- Parallax — revue globale (2026-09-12) : unicité des noms de serveur
--
-- Rien n'empêchait deux serveurs réels de porter le même nom physique ou le
-- même nom d'hôte : l'import de création ne le vérifiait pas, et la mise à
-- jour en masse (v3.5) s'appuie précisément sur ces noms comme clés de
-- rapprochement. L'unicité vaut par seau : deux hypothèses de scénarios
-- différents peuvent matérialiser le même « ELK-01 », le réel non.
CREATE UNIQUE INDEX idx_serveur_physical_name_unique
    ON serveur (physical_name, COALESCE(scenario_id, -1))
    WHERE physical_name IS NOT NULL AND physical_name <> '';

CREATE UNIQUE INDEX idx_serveur_hostname_unique
    ON serveur (hostname, COALESCE(scenario_id, -1))
    WHERE hostname IS NOT NULL AND hostname <> '';
