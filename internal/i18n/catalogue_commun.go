package i18n

// Clés partagées par tous les écrans : navigation, actions CRUD communes
// aux six référentiels (voir le patron documenté dans
// internal/web/templates/projets/liste.html), et l'écran de connexion.
// Tout nouveau fichier catalogue_*.go doit réutiliser ces clés "commun.*"
// plutôt qu'en redéclarer des équivalentes.
func init() {
	Ajouter(map[string]string{
		// -------------------------------------------------------- commun
		"commun.code":        "Code",
		"commun.libelle":     "Libellé",
		"commun.statut":      "Statut",
		"commun.actif":       "actif",
		"commun.archive":     "archivé",
		"commun.ajouter":     "Ajouter",
		"commun.modifier":    "Modifier",
		"commun.archiver":    "Archiver",
		"commun.reactiver":   "Réactiver",
		"commun.enregistrer": "Enregistrer",
		"commun.annuler":     "Annuler",
		"commun.supprimer":   "Supprimer",
		"commun.deconnexion": "Déconnexion",

		// ----------------------------------------------------- navigation
		"nav.groupe.referentiels":   "Référentiels",
		"nav.groupe.catalogue":      "Catalogue",
		"nav.groupe.inventaire":     "Inventaire",
		"nav.groupe.simulation":     "Simulation",
		"nav.groupe.formules":       "Moteur de formules",
		"nav.groupe.exploitation":   "Exploitation",
		"nav.groupe.administration": "Administration",
		"nav.projets":               "Projets",
		"nav.environnements":        "Environnements",
		"nav.technos":               "Technos",
		"nav.tiers":                 "Tiers",
		"nav.usages":                "Usages",
		"nav.zones":                 "Zones",
		"nav.modeles":               "Modèles",
		"nav.clusters":              "Clusters",
		"nav.serveurs":              "Serveurs",
		"nav.vlans":                 "VLAN",
		"nav.anomalies":             "Anomalies réseau",
		"nav.scenarios":             "Scénarios",
		"nav.comparaison":           "Comparaison",
		"nav.metriques":             "Métriques",
		"nav.variables":             "Variables",
		"nav.regles":                "Règles",
		"nav.licences":              "Licences",
		"nav.vues":                  "Vues",
		"nav.demandes":              "Demandes",
		"nav.import":                "Import",
		"nav.comptes":               "Comptes",
		"nav.gabarit_demandes":      "Gabarit demandes",
		"nav.journal":               "Journal",

		// ----------------------------------------------------- connexion
		"connexion.sous_titre":   "Inventaire matériel et capacity planning",
		"connexion.identifiant":  "Identifiant",
		"connexion.mot_de_passe": "Mot de passe",
		"connexion.bouton":       "Se connecter",

		// ------------------------------------- projets (référence CRUD)
		"projets.titre":            "Projets",
		"projets.sous_titre":       "Référentiel des projets — Logs, Stream, SecOps…",
		"projets.inclure_archives": "Inclure les projets archivés",
		"projets.aucun":            "Aucun projet.",
	}, map[string]string{
		// -------------------------------------------------------- commun
		"commun.code":        "Code",
		"commun.libelle":     "Label",
		"commun.statut":      "Status",
		"commun.actif":       "active",
		"commun.archive":     "archived",
		"commun.ajouter":     "Add",
		"commun.modifier":    "Edit",
		"commun.archiver":    "Archive",
		"commun.reactiver":   "Reactivate",
		"commun.enregistrer": "Save",
		"commun.annuler":     "Cancel",
		"commun.supprimer":   "Delete",
		"commun.deconnexion": "Log out",

		// ----------------------------------------------------- navigation
		"nav.groupe.referentiels":   "Reference data",
		"nav.groupe.catalogue":      "Catalogue",
		"nav.groupe.inventaire":     "Inventory",
		"nav.groupe.simulation":     "Simulation",
		"nav.groupe.formules":       "Capacity engine",
		"nav.groupe.exploitation":   "Operations",
		"nav.groupe.administration": "Administration",
		"nav.projets":               "Projects",
		"nav.environnements":        "Environments",
		"nav.technos":               "Technologies",
		"nav.tiers":                 "Tiers",
		"nav.usages":                "Usages",
		"nav.zones":                 "Zones",
		"nav.modeles":               "Models",
		"nav.clusters":              "Clusters",
		"nav.serveurs":              "Servers",
		"nav.vlans":                 "VLANs",
		"nav.anomalies":             "Network anomalies",
		"nav.scenarios":             "Scenarios",
		"nav.comparaison":           "Comparison",
		"nav.metriques":             "Metrics",
		"nav.variables":             "Variables",
		"nav.regles":                "Rules",
		"nav.licences":              "Licenses",
		"nav.vues":                  "Views",
		"nav.demandes":              "Requests",
		"nav.import":                "Import",
		"nav.comptes":               "Accounts",
		"nav.gabarit_demandes":      "Request template",
		"nav.journal":               "Audit log",

		// ----------------------------------------------------- connexion
		"connexion.sous_titre":   "Hardware inventory and capacity planning",
		"connexion.identifiant":  "Username",
		"connexion.mot_de_passe": "Password",
		"connexion.bouton":       "Sign in",

		// ------------------------------------- projets (CRUD reference)
		"projets.titre":            "Projects",
		"projets.sous_titre":       "Project reference list — Logs, Stream, SecOps…",
		"projets.inclure_archives": "Include archived projects",
		"projets.aucun":            "No projects.",
	})
}
