package i18n

// Clés du jeu de données d'exemple téléchargeable (handlers_exemples.go,
// page /import et cartes de format de chaque écran d'import).
func init() {
	Ajouter(map[string]string{
		"import.accueil.exemples_titre":     "Jeu de données d'exemple",
		"import.accueil.exemples_desc":      "Les cinq fichiers suivants forment un jeu cohérent — mêmes codes d'un fichier à l'autre. Importés dans cet ordre sur une base neuve, ils donnent tout de suite quelque chose à explorer.",
		"import.commun.telecharger_exemple": "Télécharger un exemple complet",
	}, map[string]string{
		"import.accueil.exemples_titre":     "Example dataset",
		"import.accueil.exemples_desc":      "The five files below form a coherent set — matching codes from one file to the next. Imported in this order on a fresh database, they give you something to explore right away.",
		"import.commun.telecharger_exemple": "Download a full example",
	})
}
