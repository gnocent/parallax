package web

import "net/http"

// routesImport n'enregistre que la page d'atterrissage /import : les quatre
// importeurs (référentiels, clusters, modèles, serveurs) enregistrent leurs
// propres routes dans leurs fichiers handlers_import_*.go.
func (s *serveur) routesImport() {
	s.mux.HandleFunc("GET /import", s.lecteur(s.importPage))
}

func (s *serveur) importPage(w http.ResponseWriter, r *http.Request) {
	s.rendrePage(w, r, s.titre(r, "titre.import"), "import_page", nil)
}
