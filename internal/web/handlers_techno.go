package web

import (
	"net/http"

	"parallax/internal/depot"
)

// routesTechnos réplique le patron de référence (voir handlers_projet.go) à
// l'identique : Techno est la seule des cinq entités de ce lot à porter un
// drapeau d'activité, comme Projet.
func (s *serveur) routesTechnos() {
	s.mux.HandleFunc("GET /technos", s.lecteur(s.technosPage))
	s.mux.HandleFunc("GET /technos/tableau", s.lecteur(s.technosTableau))
	s.mux.HandleFunc("POST /technos", s.editeur(s.technosCreer))
	s.mux.HandleFunc("GET /technos/{id}", s.lecteur(s.technosLigne))
	s.mux.HandleFunc("GET /technos/{id}/editer", s.editeur(s.technosFormulaireEdition))
	s.mux.HandleFunc("PUT /technos/{id}", s.editeur(s.technosModifier))
	s.mux.HandleFunc("POST /technos/{id}/archiver", s.editeur(s.technosArchiver))
	s.mux.HandleFunc("POST /technos/{id}/reactiver", s.editeur(s.technosReactiver))
}

// technoLigne incorpore depot.Techno et ajoute un message d'erreur optionnel
// — voir le commentaire de projetLigne dans handlers_projet.go, même raison.
type technoLigne struct {
	depot.Techno
	Erreur string
}

func technosEnLignes(technos []depot.Techno) []technoLigne {
	out := make([]technoLigne, len(technos))
	for i, t := range technos {
		out[i] = technoLigne{Techno: t}
	}
	return out
}

func (s *serveur) technosPage(w http.ResponseWriter, r *http.Request) {
	technos, err := s.depot.ListerTechnos(inclureInactifs(r))
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	// export universel (tableau.go) : mêmes données, même filtre que la page.
	if s.exporter(w, r, func() (tableau, error) {
		t := tableau{Titre: "Technos", Colonnes: []string{"Code", "Libellé", "Actif"}}
		for _, tech := range technos {
			t.Lignes = append(t.Lignes, []any{tech.Code, tech.Libelle, tech.Actif})
		}
		return t, nil
	}) {
		return
	}
	s.rendrePage(w, r, s.titre(r, "titre.technos"), "technos_page", map[string]any{
		"Technos": technosEnLignes(technos), "Export": exportTechnos(r),
	})
}

// exportTechnos : liens d'export posés dans le fragment, vers la page, avec
// l'état de la case « inclure les archivées » (voir exportProjets).
func exportTechnos(r *http.Request) exportLiens {
	return liensExportVers(r, "/technos", "", "inclure_inactifs")
}

func (s *serveur) technosTableau(w http.ResponseWriter, r *http.Request) {
	technos, err := s.depot.ListerTechnos(inclureInactifs(r))
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	s.rendreFragment(w, r, "technos_tableau", map[string]any{
		"Technos": technosEnLignes(technos), "Export": exportTechnos(r),
	})
}

// technosCreer renvoie toujours le tableau entier — voir projetsCreer.
func (s *serveur) technosCreer(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "formulaire illisible", http.StatusBadRequest)
		return
	}
	_, err := s.depotPour(r).CreerTechno(depot.Techno{
		Code: r.FormValue("code"), Libelle: r.FormValue("libelle"),
	})
	if err != nil && !erreurMetier(err) {
		s.erreurServeur(w, r, err)
		return
	}

	technos, errListe := s.depot.ListerTechnos(inclureInactifs(r))
	if errListe != nil {
		s.erreurServeur(w, r, errListe)
		return
	}
	s.rendreFragment(w, r, "technos_tableau", map[string]any{
		"Technos": technosEnLignes(technos), "Erreur": messageUtilisateur(err), "Export": exportTechnos(r),
	})
}

func (s *serveur) technosLigne(w http.ResponseWriter, r *http.Request) {
	id, err := idChemin(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	t, err := s.depot.LireTechno(id)
	if err != nil {
		s.repondreIntrouvable(w, r, err)
		return
	}
	s.rendreFragment(w, r, "technos_ligne", technoLigne{Techno: t})
}

func (s *serveur) technosFormulaireEdition(w http.ResponseWriter, r *http.Request) {
	id, err := idChemin(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	t, err := s.depot.LireTechno(id)
	if err != nil {
		s.repondreIntrouvable(w, r, err)
		return
	}
	s.rendreFragment(w, r, "technos_ligne_edition", technoLigne{Techno: t})
}

func (s *serveur) technosModifier(w http.ResponseWriter, r *http.Request) {
	id, err := idChemin(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "formulaire illisible", http.StatusBadRequest)
		return
	}
	t := depot.Techno{ID: id, Code: r.FormValue("code"), Libelle: r.FormValue("libelle")}
	if err := s.depotPour(r).ModifierTechno(t); err != nil {
		if !erreurMetier(err) {
			s.erreurServeur(w, r, err)
			return
		}
		// on réaffiche le formulaire d'édition avec l'erreur, l'utilisateur ne
		// perd pas sa saisie. ModifierTechno ne touche pas actif : on va le
		// rechercher pour ne pas afficher un statut faux (t.Actif serait la
		// valeur zéro Go, donc "archivé", sur toute erreur de validation).
		actuel, errLecture := s.depot.LireTechno(id)
		if errLecture == nil {
			t.Actif = actuel.Actif
		}
		s.rendreFragment(w, r, "technos_ligne_edition", technoLigne{Techno: t, Erreur: messageUtilisateur(err)})
		return
	}
	relu, err := s.depot.LireTechno(id)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	s.rendreFragment(w, r, "technos_ligne", technoLigne{Techno: relu})
}

func (s *serveur) technosArchiver(w http.ResponseWriter, r *http.Request) {
	s.technosBasculerActivite(w, r, s.depotPour(r).ArchiverTechno)
}

func (s *serveur) technosReactiver(w http.ResponseWriter, r *http.Request) {
	s.technosBasculerActivite(w, r, s.depotPour(r).ReactiverTechno)
}

func (s *serveur) technosBasculerActivite(w http.ResponseWriter, r *http.Request, action func(int64) error) {
	id, err := idChemin(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := action(id); err != nil && !erreurMetier(err) {
		s.erreurServeur(w, r, err)
		return
	}
	t, err := s.depot.LireTechno(id)
	if err != nil {
		s.repondreIntrouvable(w, r, err)
		return
	}
	s.rendreFragment(w, r, "technos_ligne", technoLigne{Techno: t})
}
