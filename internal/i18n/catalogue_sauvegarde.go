package i18n

// Clés de l'écran /parametres/sauvegarde (backlog v3.7) — voir
// internal/web/handlers_sauvegarde.go et templates/parametres/sauvegarde.html.
func init() {
	Ajouter(map[string]string{
		"nav.sauvegarde":              "Sauvegarde",
		"titre.parametres_sauvegarde": "Sauvegarde",

		"sauvegarde.titre":             "Sauvegarde",
		"sauvegarde.sous_titre":        "Une copie locale de la base, tous les jours à l'heure choisie — seulement si quelque chose a changé depuis la précédente. Les copies plus vieilles que la rétention réglée sont supprimées automatiquement.",
		"sauvegarde.statut_active":     "active",
		"sauvegarde.statut_inactive":   "désactivée",
		"sauvegarde.derniere_reussite": "Dernière sauvegarde écrite le",
		"sauvegarde.label_dossier":     "Dossier de destination",
		"sauvegarde.label_heure":       "Heure quotidienne",
		"sauvegarde.label_retention":   "Rétention (jours)",
		"sauvegarde.aide_dossier":      "Le dossier est créé s'il n'existe pas encore. Vide, la sauvegarde automatique reste désactivée.",
		"sauvegarde.maintenant":        "Sauvegarder maintenant",
		"sauvegarde.fichiers_titre":    "Sauvegardes présentes sur le disque",
		"sauvegarde.table_fichier":     "Fichier",
		"sauvegarde.table_taille":      "Taille",
		"sauvegarde.table_date":        "Écrit le",
		"sauvegarde.aucun_fichier":     "Aucune sauvegarde locale pour l'instant.",
	}, map[string]string{
		"nav.sauvegarde":              "Backup",
		"titre.parametres_sauvegarde": "Backup",

		"sauvegarde.titre":             "Backup",
		"sauvegarde.sous_titre":        "A local copy of the database, every day at the chosen time — only if something changed since the previous one. Copies older than the configured retention are deleted automatically.",
		"sauvegarde.statut_active":     "active",
		"sauvegarde.statut_inactive":   "disabled",
		"sauvegarde.derniere_reussite": "Last backup written on",
		"sauvegarde.label_dossier":     "Destination folder",
		"sauvegarde.label_heure":       "Daily time",
		"sauvegarde.label_retention":   "Retention (days)",
		"sauvegarde.aide_dossier":      "The folder is created if it doesn't exist yet. Left empty, automatic backup stays disabled.",
		"sauvegarde.maintenant":        "Back up now",
		"sauvegarde.fichiers_titre":    "Backups currently on disk",
		"sauvegarde.table_fichier":     "File",
		"sauvegarde.table_taille":      "Size",
		"sauvegarde.table_date":        "Written on",
		"sauvegarde.aucun_fichier":     "No local backup yet.",
	})
}
