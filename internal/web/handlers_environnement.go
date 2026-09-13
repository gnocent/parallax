package web

import (
	"net/http"
	"strconv"

	"parallax/internal/depot"
)

// routesEnvironnements réplique le patron de référence (voir
// handlers_projet.go), simplifié : Environnement ne porte pas de drapeau
// d'activité, donc pas de routes archiver/reactiver ni de case "inclure les
// archivés". Champ Ordre en plus, entier de rang d'affichage.
func (s *serveur) routesEnvironnements() {
	s.mux.HandleFunc("GET /environnements", s.lecteur(s.environnementsPage))
	s.mux.HandleFunc("GET /environnements/tableau", s.lecteur(s.environnementsTableau))
	s.mux.HandleFunc("POST /environnements", s.editeur(s.environnementsCreer))
	s.mux.HandleFunc("GET /environnements/{id}", s.lecteur(s.environnementsLigne))
	s.mux.HandleFunc("GET /environnements/{id}/editer", s.editeur(s.environnementsFormulaireEdition))
	s.mux.HandleFunc("PUT /environnements/{id}", s.editeur(s.environnementsModifier))
}

// environnementLigne incorpore depot.Environnement et ajoute un message
// d'erreur optionnel — voir le commentaire de projetLigne dans
// handlers_projet.go, même raison.
type environnementLigne struct {
	depot.Environnement
	Erreur string
}

func environnementsEnLignes(environnements []depot.Environnement) []environnementLigne {
	out := make([]environnementLigne, len(environnements))
	for i, e := range environnements {
		out[i] = environnementLigne{Environnement: e}
	}
	return out
}

func (s *serveur) environnementsPage(w http.ResponseWriter, r *http.Request) {
	environnements, err := s.depot.ListerEnvironnements()
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	// export universel (tableau.go) : mêmes données que la page.
	if s.exporter(w, r, func() (tableau, error) {
		t := tableau{Titre: "Environnements", Colonnes: []string{"Code", "Libellé", "Ordre"}}
		for _, e := range environnements {
			t.Lignes = append(t.Lignes, []any{e.Code, e.Libelle, e.Ordre})
		}
		return t, nil
	}) {
		return
	}
	s.rendrePage(w, r, s.titre(r, "titre.environnements"), "environnements_page", map[string]any{
		"Environnements": environnementsEnLignes(environnements), "Export": exportEnvironnements(r),
	})
}

// exportEnvironnements : liens d'export posés dans le fragment, vers la page
// (voir exportProjets). Pas de filtre sur cet écran.
func exportEnvironnements(r *http.Request) exportLiens {
	return liensExportVers(r, "/environnements", "")
}

func (s *serveur) environnementsTableau(w http.ResponseWriter, r *http.Request) {
	environnements, err := s.depot.ListerEnvironnements()
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	s.rendreFragment(w, r, "environnements_tableau", map[string]any{
		"Environnements": environnementsEnLignes(environnements), "Export": exportEnvironnements(r),
	})
}

// environnementsCreer renvoie toujours le tableau entier — voir projetsCreer.
func (s *serveur) environnementsCreer(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "formulaire illisible", http.StatusBadRequest)
		return
	}
	ordre, _ := strconv.Atoi(r.FormValue("ordre"))
	_, err := s.depotPour(r).CreerEnvironnement(depot.Environnement{
		Code: r.FormValue("code"), Libelle: r.FormValue("libelle"), Ordre: ordre,
	})
	if err != nil && !erreurMetier(err) {
		s.erreurServeur(w, r, err)
		return
	}

	environnements, errListe := s.depot.ListerEnvironnements()
	if errListe != nil {
		s.erreurServeur(w, r, errListe)
		return
	}
	s.rendreFragment(w, r, "environnements_tableau", map[string]any{
		"Environnements": environnementsEnLignes(environnements), "Erreur": messageUtilisateur(err),
		"Export": exportEnvironnements(r),
	})
}

func (s *serveur) environnementsLigne(w http.ResponseWriter, r *http.Request) {
	id, err := idChemin(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	e, err := s.depot.LireEnvironnement(id)
	if err != nil {
		s.repondreIntrouvable(w, r, err)
		return
	}
	s.rendreFragment(w, r, "environnements_ligne", environnementLigne{Environnement: e})
}

func (s *serveur) environnementsFormulaireEdition(w http.ResponseWriter, r *http.Request) {
	id, err := idChemin(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	e, err := s.depot.LireEnvironnement(id)
	if err != nil {
		s.repondreIntrouvable(w, r, err)
		return
	}
	s.rendreFragment(w, r, "environnements_ligne_edition", environnementLigne{Environnement: e})
}

func (s *serveur) environnementsModifier(w http.ResponseWriter, r *http.Request) {
	id, err := idChemin(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "formulaire illisible", http.StatusBadRequest)
		return
	}
	ordre, _ := strconv.Atoi(r.FormValue("ordre"))
	e := depot.Environnement{ID: id, Code: r.FormValue("code"), Libelle: r.FormValue("libelle"), Ordre: ordre}
	if err := s.depotPour(r).ModifierEnvironnement(e); err != nil {
		if !erreurMetier(err) {
			s.erreurServeur(w, r, err)
			return
		}
		s.rendreFragment(w, r, "environnements_ligne_edition",
			environnementLigne{Environnement: e, Erreur: messageUtilisateur(err)})
		return
	}
	relu, err := s.depot.LireEnvironnement(id)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	s.rendreFragment(w, r, "environnements_ligne", environnementLigne{Environnement: relu})
}
