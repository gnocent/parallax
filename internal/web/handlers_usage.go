package web

import (
	"net/http"

	"parallax/internal/depot"
)

// routesUsages réplique le patron de référence (voir handlers_projet.go),
// simplifié : UsageFonctionnel ne porte ni drapeau d'activité ni ordre
// d'affichage, seulement code et libellé.
func (s *serveur) routesUsages() {
	s.mux.HandleFunc("GET /usages", s.lecteur(s.usagesPage))
	s.mux.HandleFunc("GET /usages/tableau", s.lecteur(s.usagesTableau))
	s.mux.HandleFunc("POST /usages", s.editeur(s.usagesCreer))
	s.mux.HandleFunc("GET /usages/{id}", s.lecteur(s.usagesLigne))
	s.mux.HandleFunc("GET /usages/{id}/editer", s.editeur(s.usagesFormulaireEdition))
	s.mux.HandleFunc("PUT /usages/{id}", s.editeur(s.usagesModifier))
}

// usageLigne incorpore depot.UsageFonctionnel et ajoute un message d'erreur
// optionnel — voir le commentaire de projetLigne dans handlers_projet.go,
// même raison.
type usageLigne struct {
	depot.UsageFonctionnel
	Erreur string
}

func usagesEnLignes(usages []depot.UsageFonctionnel) []usageLigne {
	out := make([]usageLigne, len(usages))
	for i, u := range usages {
		out[i] = usageLigne{UsageFonctionnel: u}
	}
	return out
}

func (s *serveur) usagesPage(w http.ResponseWriter, r *http.Request) {
	usages, err := s.depot.ListerUsages()
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	// export universel (tableau.go) : mêmes données que la page.
	if s.exporter(w, r, func() (tableau, error) {
		t := tableau{Titre: "Usages", Colonnes: []string{"Code", "Libellé"}}
		for _, u := range usages {
			t.Lignes = append(t.Lignes, []any{u.Code, u.Libelle})
		}
		return t, nil
	}) {
		return
	}
	s.rendrePage(w, r, s.titre(r, "titre.usages"), "usages_page", map[string]any{
		"Usages": usagesEnLignes(usages), "Export": exportUsages(r),
	})
}

// exportUsages : liens d'export posés dans le fragment, vers la page (voir
// exportProjets). Pas de filtre sur cet écran.
func exportUsages(r *http.Request) exportLiens {
	return liensExportVers(r, "/usages", "")
}

func (s *serveur) usagesTableau(w http.ResponseWriter, r *http.Request) {
	usages, err := s.depot.ListerUsages()
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	s.rendreFragment(w, r, "usages_tableau", map[string]any{
		"Usages": usagesEnLignes(usages), "Export": exportUsages(r),
	})
}

// usagesCreer renvoie toujours le tableau entier — voir projetsCreer.
func (s *serveur) usagesCreer(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "formulaire illisible", http.StatusBadRequest)
		return
	}
	_, err := s.depotPour(r).CreerUsage(depot.UsageFonctionnel{
		Code: r.FormValue("code"), Libelle: r.FormValue("libelle"),
	})
	if err != nil && !erreurMetier(err) {
		s.erreurServeur(w, r, err)
		return
	}

	usages, errListe := s.depot.ListerUsages()
	if errListe != nil {
		s.erreurServeur(w, r, errListe)
		return
	}
	s.rendreFragment(w, r, "usages_tableau", map[string]any{
		"Usages": usagesEnLignes(usages), "Erreur": messageUtilisateur(err), "Export": exportUsages(r),
	})
}

func (s *serveur) usagesLigne(w http.ResponseWriter, r *http.Request) {
	id, err := idChemin(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	u, err := s.depot.LireUsage(id)
	if err != nil {
		s.repondreIntrouvable(w, r, err)
		return
	}
	s.rendreFragment(w, r, "usages_ligne", usageLigne{UsageFonctionnel: u})
}

func (s *serveur) usagesFormulaireEdition(w http.ResponseWriter, r *http.Request) {
	id, err := idChemin(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	u, err := s.depot.LireUsage(id)
	if err != nil {
		s.repondreIntrouvable(w, r, err)
		return
	}
	s.rendreFragment(w, r, "usages_ligne_edition", usageLigne{UsageFonctionnel: u})
}

func (s *serveur) usagesModifier(w http.ResponseWriter, r *http.Request) {
	id, err := idChemin(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "formulaire illisible", http.StatusBadRequest)
		return
	}
	u := depot.UsageFonctionnel{ID: id, Code: r.FormValue("code"), Libelle: r.FormValue("libelle")}
	if err := s.depotPour(r).ModifierUsage(u); err != nil {
		if !erreurMetier(err) {
			s.erreurServeur(w, r, err)
			return
		}
		s.rendreFragment(w, r, "usages_ligne_edition", usageLigne{UsageFonctionnel: u, Erreur: messageUtilisateur(err)})
		return
	}
	relu, err := s.depot.LireUsage(id)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	s.rendreFragment(w, r, "usages_ligne", usageLigne{UsageFonctionnel: relu})
}
