package web

import (
	"net/http"

	"parallax/internal/depot"
)

// routesProjets est le patron de référence pour un écran CRUD de référentiel.
// Toute nouvelle entité (environnement, techno, tier, usage, zone…)
// réplique cette structure de routes et les handlers ci-dessous à l'identique
// dans son propre fichier, en changeant seulement le nom de l'entité et ses
// champs.
func (s *serveur) routesProjets() {
	s.mux.HandleFunc("GET /projets", s.lecteur(s.projetsPage))
	s.mux.HandleFunc("GET /projets/tableau", s.lecteur(s.projetsTableau))
	s.mux.HandleFunc("POST /projets", s.editeur(s.projetsCreer))
	s.mux.HandleFunc("GET /projets/{id}", s.lecteur(s.projetsLigne))
	s.mux.HandleFunc("GET /projets/{id}/editer", s.editeur(s.projetsFormulaireEdition))
	s.mux.HandleFunc("PUT /projets/{id}", s.editeur(s.projetsModifier))
	s.mux.HandleFunc("POST /projets/{id}/archiver", s.editeur(s.projetsArchiver))
	s.mux.HandleFunc("POST /projets/{id}/reactiver", s.editeur(s.projetsReactiver))
}

// projetLigne incorpore depot.Projet et ajoute un message d'erreur optionnel,
// affiché par les gabarits projets_ligne et projets_ligne_edition. Toujours
// utiliser ce type (Erreur vide en cas de succès) plutôt que le depot.Projet
// nu : {{if .Erreur}} sur un depot.Projet ferait échouer l'exécution du
// gabarit (« can't evaluate field Erreur »), pas silencieusement — la moitié
// de la ligne déjà écrite resterait affichée, suivie du texte brut de
// l'erreur serveur. C'est pour ça que projetsEnLignes existe : ne jamais
// passer []depot.Projet directement à "projets_tableau".
type projetLigne struct {
	depot.Projet
	Erreur string
}

func projetsEnLignes(projets []depot.Projet) []projetLigne {
	out := make([]projetLigne, len(projets))
	for i, p := range projets {
		out[i] = projetLigne{Projet: p}
	}
	return out
}

func (s *serveur) projetsPage(w http.ResponseWriter, r *http.Request) {
	projets, err := s.depot.ListerProjets(inclureInactifs(r))
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	// export universel (tableau.go) : mêmes données, mêmes filtres que la page.
	if s.exporter(w, r, func() (tableau, error) {
		t := tableau{Titre: "Projets", Colonnes: []string{"Code", "Libellé", "Actif"}}
		for _, p := range projets {
			t.Lignes = append(t.Lignes, []any{p.Code, p.Libelle, p.Actif})
		}
		return t, nil
	}) {
		return
	}
	s.rendrePage(w, r, s.titre(r, "titre.projets"), "projets_page", map[string]any{
		"Projets": projetsEnLignes(projets), "Export": exportProjets(r),
	})
}

// exportProjets construit les liens d'export du tableau des projets. Les
// boutons vivent dans le fragment "projets_tableau" (rechargé par la case
// « inclure les archivés » et par la création) et visent toujours la page
// /projets avec l'état courant de la case : c'est projetsPage qui répond.
func exportProjets(r *http.Request) exportLiens {
	return liensExportVers(r, "/projets", "", "inclure_inactifs")
}

func (s *serveur) projetsTableau(w http.ResponseWriter, r *http.Request) {
	projets, err := s.depot.ListerProjets(inclureInactifs(r))
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	s.rendreFragment(w, r, "projets_tableau", map[string]any{
		"Projets": projetsEnLignes(projets), "Export": exportProjets(r),
	})
}

// projetsCreer renvoie toujours le tableau entier (jamais 4xx) : en cas
// d'erreur métier, le tableau réapparaît inchangé avec un message ; en cas de
// succès, la nouvelle ligne y figure. Éviter de ne renvoyer que la ligne créée
// : quand la liste était vide, la ligne « Aucun projet » resterait affichée à
// côté.
func (s *serveur) projetsCreer(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "formulaire illisible", http.StatusBadRequest)
		return
	}
	_, err := s.depotPour(r).CreerProjet(depot.Projet{
		Code: r.FormValue("code"), Libelle: r.FormValue("libelle"),
	})
	if err != nil && !erreurMetier(err) {
		s.erreurServeur(w, r, err)
		return
	}

	projets, errListe := s.depot.ListerProjets(inclureInactifs(r))
	if errListe != nil {
		s.erreurServeur(w, r, errListe)
		return
	}
	s.rendreFragment(w, r, "projets_tableau", map[string]any{
		"Projets": projetsEnLignes(projets), "Erreur": messageUtilisateur(err), "Export": exportProjets(r),
	})
}

func (s *serveur) projetsLigne(w http.ResponseWriter, r *http.Request) {
	id, err := idChemin(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	p, err := s.depot.LireProjet(id)
	if err != nil {
		s.repondreIntrouvable(w, r, err)
		return
	}
	s.rendreFragment(w, r, "projets_ligne", projetLigne{Projet: p})
}

func (s *serveur) projetsFormulaireEdition(w http.ResponseWriter, r *http.Request) {
	id, err := idChemin(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	p, err := s.depot.LireProjet(id)
	if err != nil {
		s.repondreIntrouvable(w, r, err)
		return
	}
	s.rendreFragment(w, r, "projets_ligne_edition", projetLigne{Projet: p})
}

func (s *serveur) projetsModifier(w http.ResponseWriter, r *http.Request) {
	id, err := idChemin(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "formulaire illisible", http.StatusBadRequest)
		return
	}
	p := depot.Projet{ID: id, Code: r.FormValue("code"), Libelle: r.FormValue("libelle")}
	if err := s.depotPour(r).ModifierProjet(p); err != nil {
		if !erreurMetier(err) {
			s.erreurServeur(w, r, err)
			return
		}
		// on réaffiche le formulaire d'édition avec l'erreur, l'utilisateur ne
		// perd pas sa saisie. ModifierProjet ne touche pas actif : on va le
		// rechercher pour ne pas afficher un statut faux (p.Actif serait la
		// valeur zéro Go, donc "archivé", sur toute erreur de validation).
		actuel, errLecture := s.depot.LireProjet(id)
		if errLecture == nil {
			p.Actif = actuel.Actif
		}
		s.rendreFragment(w, r, "projets_ligne_edition", projetLigne{Projet: p, Erreur: messageUtilisateur(err)})
		return
	}
	relu, err := s.depot.LireProjet(id)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	s.rendreFragment(w, r, "projets_ligne", projetLigne{Projet: relu})
}

func (s *serveur) projetsArchiver(w http.ResponseWriter, r *http.Request) {
	s.projetsBasculerActivite(w, r, s.depotPour(r).ArchiverProjet)
}

func (s *serveur) projetsReactiver(w http.ResponseWriter, r *http.Request) {
	s.projetsBasculerActivite(w, r, s.depotPour(r).ReactiverProjet)
}

func (s *serveur) projetsBasculerActivite(w http.ResponseWriter, r *http.Request, action func(int64) error) {
	id, err := idChemin(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := action(id); err != nil && !erreurMetier(err) {
		s.erreurServeur(w, r, err)
		return
	}
	p, err := s.depot.LireProjet(id)
	if err != nil {
		s.repondreIntrouvable(w, r, err)
		return
	}
	s.rendreFragment(w, r, "projets_ligne", projetLigne{Projet: p})
}
