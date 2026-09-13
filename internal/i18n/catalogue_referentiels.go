package i18n

// Clés propres aux cinq écrans de référentiel calqués sur le patron projets
// (internal/web/templates/projets/liste.html) : environnements, technos,
// tiers, usages, zones. Les libellés communs à tous (code, libellé, statut,
// actions CRUD…) vivent dans catalogue_commun.go et sont réutilisés tels
// quels ici, jamais redéclarés.
func init() {
	Ajouter(map[string]string{
		// ----------------------------------------------------- environnements
		"environnements.titre":      "Environnements",
		"environnements.sous_titre": "Référentiel des environnements — PROD, QUAL, DEV…",
		"environnements.aucun":      "Aucun environnement.",
		"environnements.ordre":      "Ordre",

		// ------------------------------------------------------------ technos
		"technos.titre":            "Technos",
		"technos.sous_titre":       "Référentiel des technologies suivies — Elasticsearch, Kafka…",
		"technos.inclure_archives": "Inclure les technos archivées",
		"technos.aucun":            "Aucune techno.",

		// -------------------------------------------------------------- tiers
		"tiers.titre":      "Tiers",
		"tiers.sous_titre": "Référentiel des niveaux de service — HOT, WARM, COLD…",
		"tiers.aucun":      "Aucun tier.",

		// ------------------------------------------------------------- usages
		"usages.titre":      "Usages fonctionnels",
		"usages.sous_titre": "Référentiel des usages métier — log management, APM, métrologie…",
		"usages.aucun":      "Aucun usage.",

		// -------------------------------------------------------------- zones
		"zones.titre":      "Zones",
		"zones.sous_titre": "Référentiel des zones — portée des contraintes de répartition d'un cluster.",
		"zones.aucun":      "Aucune zone.",
		"zones.site":       "Site",
	}, map[string]string{
		// ----------------------------------------------------- environnements
		"environnements.titre":      "Environments",
		"environnements.sous_titre": "Environment reference list — PROD, QUAL, DEV…",
		"environnements.aucun":      "No environments.",
		"environnements.ordre":      "Order",

		// ------------------------------------------------------------ technos
		"technos.titre":            "Technologies",
		"technos.sous_titre":       "Reference list of tracked technologies — Elasticsearch, Kafka…",
		"technos.inclure_archives": "Include archived technologies",
		"technos.aucun":            "No technologies.",

		// -------------------------------------------------------------- tiers
		"tiers.titre":      "Tiers",
		"tiers.sous_titre": "Service tier reference list — HOT, WARM, COLD…",
		"tiers.aucun":      "No tiers.",

		// ------------------------------------------------------------- usages
		"usages.titre":      "Functional usages",
		"usages.sous_titre": "Business usage reference list — log management, APM, metrics…",
		"usages.aucun":      "No usages.",

		// -------------------------------------------------------------- zones
		"zones.titre":      "Zones",
		"zones.sous_titre": "Zone reference list — the scope of a cluster's distribution constraints.",
		"zones.aucun":      "No zones.",
		"zones.site":       "Site",
	})
}
