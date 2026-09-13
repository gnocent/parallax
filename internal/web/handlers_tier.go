package web

import (
	"net/http"
	"strconv"

	"parallax/internal/depot"
)

// routesTiers réplique le patron de référence (voir handlers_projet.go),
// simplifié comme Environnement : pas de drapeau d'activité, champ Ordre en
// plus.
func (s *serveur) routesTiers() {
	s.mux.HandleFunc("GET /tiers", s.lecteur(s.tiersPage))
	s.mux.HandleFunc("GET /tiers/tableau", s.lecteur(s.tiersTableau))
	s.mux.HandleFunc("POST /tiers", s.editeur(s.tiersCreer))
	s.mux.HandleFunc("GET /tiers/{id}", s.lecteur(s.tiersLigne))
	s.mux.HandleFunc("GET /tiers/{id}/editer", s.editeur(s.tiersFormulaireEdition))
	s.mux.HandleFunc("PUT /tiers/{id}", s.editeur(s.tiersModifier))
}

// tierLigne incorpore depot.Tier et ajoute un message d'erreur optionnel —
// voir le commentaire de projetLigne dans handlers_projet.go, même raison.
type tierLigne struct {
	depot.Tier
	Erreur string
}

func tiersEnLignes(tiers []depot.Tier) []tierLigne {
	out := make([]tierLigne, len(tiers))
	for i, t := range tiers {
		out[i] = tierLigne{Tier: t}
	}
	return out
}

func (s *serveur) tiersPage(w http.ResponseWriter, r *http.Request) {
	tiers, err := s.depot.ListerTiers()
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	// export universel (tableau.go) : mêmes données que la page.
	if s.exporter(w, r, func() (tableau, error) {
		t := tableau{Titre: "Tiers", Colonnes: []string{"Code", "Libellé", "Ordre"}}
		for _, tier := range tiers {
			t.Lignes = append(t.Lignes, []any{tier.Code, tier.Libelle, tier.Ordre})
		}
		return t, nil
	}) {
		return
	}
	s.rendrePage(w, r, s.titre(r, "titre.tiers"), "tiers_page", map[string]any{
		"Tiers": tiersEnLignes(tiers), "Export": exportTiers(r),
	})
}

// exportTiers : liens d'export posés dans le fragment, vers la page (voir
// exportProjets). Pas de filtre sur cet écran.
func exportTiers(r *http.Request) exportLiens {
	return liensExportVers(r, "/tiers", "")
}

func (s *serveur) tiersTableau(w http.ResponseWriter, r *http.Request) {
	tiers, err := s.depot.ListerTiers()
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	s.rendreFragment(w, r, "tiers_tableau", map[string]any{
		"Tiers": tiersEnLignes(tiers), "Export": exportTiers(r),
	})
}

// tiersCreer renvoie toujours le tableau entier — voir projetsCreer.
func (s *serveur) tiersCreer(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "formulaire illisible", http.StatusBadRequest)
		return
	}
	ordre, _ := strconv.Atoi(r.FormValue("ordre"))
	_, err := s.depotPour(r).CreerTier(depot.Tier{
		Code: r.FormValue("code"), Libelle: r.FormValue("libelle"), Ordre: ordre,
	})
	if err != nil && !erreurMetier(err) {
		s.erreurServeur(w, r, err)
		return
	}

	tiers, errListe := s.depot.ListerTiers()
	if errListe != nil {
		s.erreurServeur(w, r, errListe)
		return
	}
	s.rendreFragment(w, r, "tiers_tableau", map[string]any{
		"Tiers": tiersEnLignes(tiers), "Erreur": messageUtilisateur(err), "Export": exportTiers(r),
	})
}

func (s *serveur) tiersLigne(w http.ResponseWriter, r *http.Request) {
	id, err := idChemin(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	t, err := s.depot.LireTier(id)
	if err != nil {
		s.repondreIntrouvable(w, r, err)
		return
	}
	s.rendreFragment(w, r, "tiers_ligne", tierLigne{Tier: t})
}

func (s *serveur) tiersFormulaireEdition(w http.ResponseWriter, r *http.Request) {
	id, err := idChemin(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	t, err := s.depot.LireTier(id)
	if err != nil {
		s.repondreIntrouvable(w, r, err)
		return
	}
	s.rendreFragment(w, r, "tiers_ligne_edition", tierLigne{Tier: t})
}

func (s *serveur) tiersModifier(w http.ResponseWriter, r *http.Request) {
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
	t := depot.Tier{ID: id, Code: r.FormValue("code"), Libelle: r.FormValue("libelle"), Ordre: ordre}
	if err := s.depotPour(r).ModifierTier(t); err != nil {
		if !erreurMetier(err) {
			s.erreurServeur(w, r, err)
			return
		}
		s.rendreFragment(w, r, "tiers_ligne_edition", tierLigne{Tier: t, Erreur: messageUtilisateur(err)})
		return
	}
	relu, err := s.depot.LireTier(id)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	s.rendreFragment(w, r, "tiers_ligne", tierLigne{Tier: relu})
}
