package i18n

// Clés "import.etapes.*" : l'indicateur à trois étapes partagé par tous les
// écrans d'import (voir templates/import/etapes.html).
func init() {
	Ajouter(map[string]string{
		"import.etapes.deposer":   "Déposer",
		"import.etapes.analyser":  "Analyser",
		"import.etapes.confirmer": "Confirmer",
	}, map[string]string{
		"import.etapes.deposer":   "Upload",
		"import.etapes.analyser":  "Analyze",
		"import.etapes.confirmer": "Confirm",
	})
}
