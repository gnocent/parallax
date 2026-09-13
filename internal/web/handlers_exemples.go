package web

import (
	"embed"
	"io/fs"
	"net/http"
	"net/url"
)

// fichiersExemples embarque les jeux de données d'exemple des cinq imports
// CSV, dans le binaire — voir docs/import-format.md, qui explique leur
// ordre : ils s'importent les uns après les autres sur une base neuve et
// donnent un jeu cohérent (mêmes codes de projet, cluster, modèle d'un
// fichier à l'autre). Objectif : télécharger le binaire suffit à essayer
// l'application avec des données réalistes, sans cloner le dépôt.
//
//go:embed exemples
var fichiersExemples embed.FS

// exemplesDisponibles associe le segment d'URL au nom réel du fichier
// embarqué : jamais le chemin de la requête ne sert directement à lire le
// FS embarqué, pour ne proposer que les cinq fichiers prévus.
var exemplesDisponibles = map[string]string{
	"referentiels": "referentiels-exemple.csv",
	"clusters":     "clusters-exemple.csv",
	"modeles":      "modeles-exemple.csv",
	"serveurs":     "serveurs-exemple.csv",
	"vlans":        "vlans-exemple.csv",
}

func (s *serveur) routesExemples() {
	s.mux.HandleFunc("GET /import/exemples/{nom}", s.lecteur(s.exempleTelecharger))
}

// exempleTelecharger sert l'un des cinq fichiers CSV d'exemple embarqués,
// avec les mêmes en-têtes que les exports de tableaux (tableau.go) : un
// téléchargement direct, jamais un rendu en ligne dans l'onglet.
func (s *serveur) exempleTelecharger(w http.ResponseWriter, r *http.Request) {
	fichier, ok := exemplesDisponibles[r.PathValue("nom")]
	if !ok {
		http.NotFound(w, r)
		return
	}
	contenu, err := fs.ReadFile(fichiersExemples, "exemples/"+fichier)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+url.PathEscape(fichier)+`"`)
	w.Write(contenu)
}
