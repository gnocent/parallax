package web

import (
	"net/http"

	"parallax/internal/depot"
)

// routesMetriques réplique le patron de référence (voir handlers_projet.go),
// simplifié comme handlers_environnement.go : une métrique ne porte pas de
// drapeau d'activité, donc pas de routes archiver/reactiver ni de case
// "inclure les archivés". Champs : Code, Libellé, Unité — tous obligatoires
// (voir depot.CreerMetrique).
func (s *serveur) routesMetriques() {
	s.mux.HandleFunc("GET /metriques", s.lecteur(s.metriquesPage))
	s.mux.HandleFunc("GET /metriques/tableau", s.lecteur(s.metriquesTableau))
	s.mux.HandleFunc("POST /metriques", s.editeur(s.metriquesCreer))
	s.mux.HandleFunc("GET /metriques/{id}", s.lecteur(s.metriquesLigne))
	s.mux.HandleFunc("GET /metriques/{id}/editer", s.editeur(s.metriquesFormulaireEdition))
	s.mux.HandleFunc("PUT /metriques/{id}", s.editeur(s.metriquesModifier))
}

// metriqueLigne incorpore depot.Metrique et ajoute un message d'erreur
// optionnel — voir le commentaire de projetLigne dans handlers_projet.go,
// même raison.
type metriqueLigne struct {
	depot.Metrique
	Erreur string
}

func metriquesEnLignes(metriques []depot.Metrique) []metriqueLigne {
	out := make([]metriqueLigne, len(metriques))
	for i, m := range metriques {
		out[i] = metriqueLigne{Metrique: m}
	}
	return out
}

func (s *serveur) metriquesPage(w http.ResponseWriter, r *http.Request) {
	metriques, err := s.depot.ListerMetriques()
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	// export universel (tableau.go) : mêmes données que la page.
	if s.exporter(w, r, func() (tableau, error) {
		t := tableau{Titre: "Métriques", Colonnes: []string{"Code", "Libellé", "Unité"}}
		for _, m := range metriques {
			t.Lignes = append(t.Lignes, []any{m.Code, m.Libelle, m.Unite})
		}
		return t, nil
	}) {
		return
	}
	s.rendrePage(w, r, s.titre(r, "titre.metriques"), "metriques_page", map[string]any{
		"Metriques": metriquesEnLignes(metriques), "Export": exportMetriques(r),
	})
}

// exportMetriques : liens d'export posés dans le fragment, vers la page (voir
// exportProjets). Pas de filtre sur cet écran.
func exportMetriques(r *http.Request) exportLiens {
	return liensExportVers(r, "/metriques", "")
}

func (s *serveur) metriquesTableau(w http.ResponseWriter, r *http.Request) {
	metriques, err := s.depot.ListerMetriques()
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	s.rendreFragment(w, r, "metriques_tableau", map[string]any{
		"Metriques": metriquesEnLignes(metriques), "Export": exportMetriques(r),
	})
}

// metriquesCreer renvoie toujours le tableau entier — voir projetsCreer.
func (s *serveur) metriquesCreer(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "formulaire illisible", http.StatusBadRequest)
		return
	}
	_, err := s.depotPour(r).CreerMetrique(depot.Metrique{
		Code: r.FormValue("code"), Libelle: r.FormValue("libelle"), Unite: r.FormValue("unite"),
	})
	if err != nil && !erreurMetier(err) {
		s.erreurServeur(w, r, err)
		return
	}

	metriques, errListe := s.depot.ListerMetriques()
	if errListe != nil {
		s.erreurServeur(w, r, errListe)
		return
	}
	s.rendreFragment(w, r, "metriques_tableau", map[string]any{
		"Metriques": metriquesEnLignes(metriques), "Erreur": messageUtilisateur(err), "Export": exportMetriques(r),
	})
}

func (s *serveur) metriquesLigne(w http.ResponseWriter, r *http.Request) {
	id, err := idChemin(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	m, err := s.depot.LireMetrique(id)
	if err != nil {
		s.repondreIntrouvable(w, r, err)
		return
	}
	s.rendreFragment(w, r, "metriques_ligne", metriqueLigne{Metrique: m})
}

func (s *serveur) metriquesFormulaireEdition(w http.ResponseWriter, r *http.Request) {
	id, err := idChemin(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	m, err := s.depot.LireMetrique(id)
	if err != nil {
		s.repondreIntrouvable(w, r, err)
		return
	}
	s.rendreFragment(w, r, "metriques_ligne_edition", metriqueLigne{Metrique: m})
}

func (s *serveur) metriquesModifier(w http.ResponseWriter, r *http.Request) {
	id, err := idChemin(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "formulaire illisible", http.StatusBadRequest)
		return
	}
	// Le code reste stable (ModifierMetrique ne l'écrase pas) : on le relit
	// pour ne pas afficher une valeur vide sur un réaffichage après erreur —
	// même précaution que projetsModifier pour Actif.
	m := depot.Metrique{ID: id, Code: r.FormValue("code"), Libelle: r.FormValue("libelle"), Unite: r.FormValue("unite")}
	if err := s.depotPour(r).ModifierMetrique(m); err != nil {
		if !erreurMetier(err) {
			s.erreurServeur(w, r, err)
			return
		}
		actuel, errLecture := s.depot.LireMetrique(id)
		if errLecture == nil {
			m.Code = actuel.Code
		}
		s.rendreFragment(w, r, "metriques_ligne_edition", metriqueLigne{Metrique: m, Erreur: messageUtilisateur(err)})
		return
	}
	relu, err := s.depot.LireMetrique(id)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	s.rendreFragment(w, r, "metriques_ligne", metriqueLigne{Metrique: relu})
}
