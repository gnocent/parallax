package web

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"parallax/internal/depot"
)

// Écran Scénarios (v2.1 du backlog) — calques de deltas posés sur le réel.
// Réplique le patron de référence (handlers_projet.go) pour le CRUD, et le
// patron de bascule d'état simple (handlers_zone.go / la partie
// activer-désactiver de handlers_regle.go) pour les transitions de statut.
// L'écran de promotion est délibérément une page complète, pas un fragment
// htmx : choisir les scénarios concurrents à abandonner est une décision
// engageante, jamais déduite automatiquement (voir Promouvoir dans
// internal/depot/scenario.go).
//
// routesScenarios() n'est pas appelée depuis server.go : l'intégration ajoute
// cet appel séparément (voir la doc de tête de middleware.go).
//
//	GET  /scenarios                    page complète (tableau + création)
//	GET  /scenarios/tableau             tableau seul (fragment)
//	POST /scenarios                    créer, répond le tableau entier
//	GET  /scenarios/{id}                 ligne en lecture (fragment)
//	GET  /scenarios/{id}/editer          ligne en édition (fragment)
//	PUT  /scenarios/{id}                 modifier, répond la ligne
//	POST /scenarios/{id}/activer         BROUILLON -> ACTIF, répond la ligne
//	POST /scenarios/{id}/abandonner      -> ABANDONNE, répond la ligne
//	POST /scenarios/{id}/reprendre       ABANDONNE -> ACTIF, répond la ligne
//	GET  /scenarios/{id}/promouvoir      page complète de promotion
//	POST /scenarios/{id}/promouvoir      applique Promouvoir, redirige ou réaffiche
func (s *serveur) routesScenarios() {
	s.mux.HandleFunc("GET /scenarios", s.lecteur(s.scenariosPage))
	s.mux.HandleFunc("GET /scenarios/tableau", s.lecteur(s.scenariosTableau))
	s.mux.HandleFunc("POST /scenarios", s.editeur(s.scenariosCreer))
	s.mux.HandleFunc("GET /scenarios/{id}", s.lecteur(s.scenariosLigne))
	s.mux.HandleFunc("GET /scenarios/{id}/editer", s.editeur(s.scenariosFormulaireEdition))
	s.mux.HandleFunc("PUT /scenarios/{id}", s.editeur(s.scenariosModifier))
	s.mux.HandleFunc("POST /scenarios/{id}/activer", s.editeur(s.scenariosActiver))
	s.mux.HandleFunc("POST /scenarios/{id}/abandonner", s.editeur(s.scenariosAbandonner))
	s.mux.HandleFunc("POST /scenarios/{id}/reprendre", s.editeur(s.scenariosReprendre))
	s.mux.HandleFunc("GET /scenarios/{id}/promouvoir", s.editeur(s.scenariosPromotionPage))
	s.mux.HandleFunc("POST /scenarios/{id}/promouvoir", s.editeur(s.scenariosPromouvoir))
}

// scenarioLigne incorpore depot.Scenario et ajoute un message d'erreur
// optionnel et le libellé résolu du projet — voir le commentaire de
// projetLigne dans handlers_projet.go, même raison : ne jamais passer un
// []depot.Scenario nu à un gabarit qui teste .Erreur.
type scenarioLigne struct {
	depot.Scenario
	Erreur        string
	ProjetLibelle string
}

// ProjetIDOuZero expose ProjetID (*int64) comme un int64 (0 = absent) pour
// que le gabarit d'édition compare directement à l'id d'une <option> avec
// eq — même principe que TierIDOuZero (clusterLigne, handlers_cluster.go) et
// ProjetIDOuZero (regleFormulaire, handlers_regle.go), dont on réutilise ici
// le helper entierOuZero.
func (l scenarioLigne) ProjetIDOuZero() int64 { return entierOuZero(l.ProjetID) }

// scenarioEdition porte, en plus de la ligne, les projets actifs à proposer
// dans le <select> d'édition — même rôle que clusterEdition (handlers_cluster.go).
type scenarioEdition struct {
	scenarioLigne
	Projets []depot.Projet
}

// chargerLibellesProjets renvoie le libellé de chaque projet (actif ou non :
// un scénario créé avant l'archivage de son projet doit quand même afficher
// un libellé plutôt qu'une case vide, même raison que chargerLibellesCluster
// dans handlers_cluster.go).
func (s *serveur) chargerLibellesProjets() (map[int64]string, error) {
	projets, err := s.depot.ListerProjets(true)
	if err != nil {
		return nil, err
	}
	m := make(map[int64]string, len(projets))
	for _, p := range projets {
		m[p.ID] = p.Libelle
	}
	return m, nil
}

func scenarioEnLigne(sc depot.Scenario, libellesProjets map[int64]string) scenarioLigne {
	l := scenarioLigne{Scenario: sc}
	if sc.ProjetID != nil {
		l.ProjetLibelle = libellesProjets[*sc.ProjetID]
	}
	return l
}

func scenariosEnLignes(scenarios []depot.Scenario, libellesProjets map[int64]string) []scenarioLigne {
	out := make([]scenarioLigne, len(scenarios))
	for i, sc := range scenarios {
		out[i] = scenarioEnLigne(sc, libellesProjets)
	}
	return out
}

// listerScenariosAffichables factorise la relecture utilisée par
// scenariosPage, scenariosTableau et scenariosCreer : la liste filtrée par
// inclureClos, plus les libellés de projet nécessaires à l'affichage.
func (s *serveur) listerScenariosAffichables(inclureClos bool) ([]depot.Scenario, map[int64]string, error) {
	scenarios, err := s.depot.ListerScenarios(inclureClos)
	if err != nil {
		return nil, nil, err
	}
	libelles, err := s.chargerLibellesProjets()
	if err != nil {
		return nil, nil, err
	}
	return scenarios, libelles, nil
}

func (s *serveur) scenariosPage(w http.ResponseWriter, r *http.Request) {
	projets, err := s.depot.ListerProjets(false)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	scenarios, libelles, err := s.listerScenariosAffichables(inclureInactifs(r))
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	lignes := scenariosEnLignes(scenarios, libelles)
	// export universel (tableau.go) : mêmes données, même case « inclure les
	// clos » que la page.
	if s.exporter(w, r, func() (tableau, error) {
		t := tableau{Titre: "Scénarios", Colonnes: []string{"Nom", "Projet", "Description", "Statut", "Créé le"}}
		for _, l := range lignes {
			t.Lignes = append(t.Lignes, []any{l.Nom, l.ProjetLibelle, l.Description, l.Statut, l.DateCreation})
		}
		return t, nil
	}) {
		return
	}
	s.rendrePage(w, r, s.titre(r, "titre.scenarios"), "scenarios_page", map[string]any{
		"Scenarios": lignes,
		"Projets":   projets,
		"Export":    exportScenarios(r),
	})
}

// exportScenarios : liens d'export posés dans le fragment, vers la page, avec
// l'état de la case « inclure les clos » (voir exportProjets, handlers_projet.go).
func exportScenarios(r *http.Request) exportLiens {
	return liensExportVers(r, "/scenarios", "", "inclure_inactifs")
}

func (s *serveur) scenariosTableau(w http.ResponseWriter, r *http.Request) {
	scenarios, libelles, err := s.listerScenariosAffichables(inclureInactifs(r))
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	s.rendreFragment(w, r, "scenarios_tableau", map[string]any{
		"Scenarios": scenariosEnLignes(scenarios, libelles), "Export": exportScenarios(r),
	})
}

// scenariosCreer renvoie toujours le tableau entier (jamais 4xx) — voir
// projetsCreer, même raison.
func (s *serveur) scenariosCreer(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "formulaire illisible", http.StatusBadRequest)
		return
	}
	projetID, err := idOptionnel(r, "projet_id")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	_, err = s.depotPour(r).CreerScenario(depot.Scenario{
		Nom: r.FormValue("nom"), Description: r.FormValue("description"), ProjetID: projetID,
	})
	if err != nil && !erreurMetier(err) {
		s.erreurServeur(w, r, err)
		return
	}

	scenarios, libelles, errListe := s.listerScenariosAffichables(inclureInactifs(r))
	if errListe != nil {
		s.erreurServeur(w, r, errListe)
		return
	}
	s.rendreFragment(w, r, "scenarios_tableau", map[string]any{
		"Scenarios": scenariosEnLignes(scenarios, libelles), "Erreur": messageUtilisateur(err),
		"Export": exportScenarios(r),
	})
}

func (s *serveur) scenariosLigne(w http.ResponseWriter, r *http.Request) {
	id, err := idChemin(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	sc, err := s.depot.LireScenario(id)
	if err != nil {
		s.repondreIntrouvable(w, r, err)
		return
	}
	libelles, err := s.chargerLibellesProjets()
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	s.rendreFragment(w, r, "scenarios_ligne", scenarioEnLigne(sc, libelles))
}

func (s *serveur) scenariosFormulaireEdition(w http.ResponseWriter, r *http.Request) {
	id, err := idChemin(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	sc, err := s.depot.LireScenario(id)
	if err != nil {
		s.repondreIntrouvable(w, r, err)
		return
	}
	projets, err := s.depot.ListerProjets(false)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	s.rendreFragment(w, r, "scenarios_ligne_edition", scenarioEdition{
		scenarioLigne: scenarioLigne{Scenario: sc}, Projets: projets,
	})
}

func (s *serveur) scenariosModifier(w http.ResponseWriter, r *http.Request) {
	id, err := idChemin(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "formulaire illisible", http.StatusBadRequest)
		return
	}
	projetID, err := idOptionnel(r, "projet_id")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	sc := depot.Scenario{
		ID: id, Nom: r.FormValue("nom"), Description: r.FormValue("description"), ProjetID: projetID,
	}
	if err := s.depotPour(r).ModifierScenario(sc); err != nil {
		if !erreurMetier(err) {
			s.erreurServeur(w, r, err)
			return
		}
		// ModifierScenario ne touche pas le statut ni les dates : les relire
		// pour ne pas afficher un statut faux (sc.Statut serait la valeur
		// zéro Go sur toute erreur de validation) — même précaution que
		// projetsModifier pour Actif.
		if actuel, errLecture := s.depot.LireScenario(id); errLecture == nil {
			sc.Statut = actuel.Statut
			sc.DateCreation = actuel.DateCreation
			sc.DateCloture = actuel.DateCloture
			sc.AuteurID = actuel.AuteurID
		}
		projets, errRef := s.depot.ListerProjets(false)
		if errRef != nil {
			s.erreurServeur(w, r, errRef)
			return
		}
		s.rendreFragment(w, r, "scenarios_ligne_edition", scenarioEdition{
			scenarioLigne: scenarioLigne{Scenario: sc, Erreur: messageUtilisateur(err)}, Projets: projets,
		})
		return
	}

	relu, err := s.depot.LireScenario(id)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	libelles, err := s.chargerLibellesProjets()
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	s.rendreFragment(w, r, "scenarios_ligne", scenarioEnLigne(relu, libelles))
}

func (s *serveur) scenariosActiver(w http.ResponseWriter, r *http.Request) {
	s.scenariosBasculerStatut(w, r, s.depotPour(r).ActiverScenario)
}

func (s *serveur) scenariosAbandonner(w http.ResponseWriter, r *http.Request) {
	s.scenariosBasculerStatut(w, r, s.depotPour(r).AbandonnerScenario)
}

func (s *serveur) scenariosReprendre(w http.ResponseWriter, r *http.Request) {
	s.scenariosBasculerStatut(w, r, s.depotPour(r).ReprendreScenario)
}

// scenariosBasculerStatut répond la ligne seule — une transition de statut ne
// change ni le nombre de lignes ni leur ordre. Une transition interdite
// (ErrValidation) affiche le message inline sur la ligne, sans rien avoir
// changé en base — même patron que reglesBasculerActivite (handlers_regle.go).
func (s *serveur) scenariosBasculerStatut(w http.ResponseWriter, r *http.Request, action func(int64) error) {
	id, err := idChemin(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	errAction := action(id)
	if errAction != nil && !erreurMetier(errAction) {
		s.erreurServeur(w, r, errAction)
		return
	}
	sc, err := s.depot.LireScenario(id)
	if err != nil {
		s.repondreIntrouvable(w, r, err)
		return
	}
	libelles, err := s.chargerLibellesProjets()
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	ligne := scenarioEnLigne(sc, libelles)
	ligne.Erreur = messageUtilisateur(errAction)
	s.rendreFragment(w, r, "scenarios_ligne", ligne)
}

// ---------------------------------------------------------------- promotion

// concurrentsPromotion renvoie tous les scénarios BROUILLON ou ACTIF autres
// que celui qu'on promeut : les candidats à cocher pour un abandon
// simultané. La liste n'est jamais filtrée par heuristique (même projet…) —
// le choix appartient entièrement à l'utilisateur (voir Promouvoir,
// internal/depot/scenario.go).
func (s *serveur) concurrentsPromotion(id int64) ([]depot.Scenario, error) {
	tous, err := s.depot.ListerScenarios(false)
	if err != nil {
		return nil, err
	}
	out := make([]depot.Scenario, 0, len(tous))
	for _, sc := range tous {
		if sc.ID != id {
			out = append(out, sc)
		}
	}
	return out, nil
}

// csrfDepuisSession republie le jeton CSRF de la session dans le contexte
// d'un gabarit de contenu : donneesPage ne l'expose qu'à mise_en_page, pas au
// contenu, donc toute page portant son propre <form method="post"> classique
// doit le republier explicitement — même raison que vuesOuvrir (handlers_vue.go).
func (s *serveur) csrfDepuisSession(r *http.Request) string {
	if session, ok := sessionDepuis(r.Context()); ok {
		return session.CSRFToken
	}
	return ""
}

func (s *serveur) rendrePagePromotion(w http.ResponseWriter, r *http.Request, sc depot.Scenario, erreur string) {
	concurrents, err := s.concurrentsPromotion(sc.ID)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	s.rendrePage(w, r, s.titre(r, "titre.promouvoir")+" "+sc.Nom, "scenario_promotion_page", map[string]any{
		"Scenario":    sc,
		"Concurrents": concurrents,
		"Erreur":      erreur,
		"CSRFToken":   s.csrfDepuisSession(r),
	})
}

func (s *serveur) scenariosPromotionPage(w http.ResponseWriter, r *http.Request) {
	id, err := idChemin(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	sc, err := s.depot.LireScenario(id)
	if err != nil {
		if errors.Is(err, depot.ErrIntrouvable) {
			http.NotFound(w, r)
			return
		}
		s.erreurServeur(w, r, err)
		return
	}
	s.rendrePagePromotion(w, r, sc, "")
}

// idsFormulaire lit les valeurs multiples d'un champ de formulaire (des
// cases à cocher répétant le même name) en []int64. Vit ici plutôt que dans
// aide_formulaire.go (existant, non modifié par cet écran) : seule la
// promotion en a besoin.
func idsFormulaire(r *http.Request, nom string) ([]int64, error) {
	valeurs := r.Form[nom]
	out := make([]int64, 0, len(valeurs))
	for _, v := range valeurs {
		id, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("%s invalide : « %s »", nom, v)
		}
		out = append(out, id)
	}
	return out, nil
}

func (s *serveur) scenariosPromouvoir(w http.ResponseWriter, r *http.Request) {
	id, err := idChemin(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "formulaire illisible", http.StatusBadRequest)
		return
	}
	abandonner, err := idsFormulaire(r, "abandonner")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := s.depotPour(r).Promouvoir(id, abandonner); err != nil {
		if !erreurMetier(err) {
			s.erreurServeur(w, r, err)
			return
		}
		sc, errLecture := s.depot.LireScenario(id)
		if errLecture != nil {
			if errors.Is(errLecture, depot.ErrIntrouvable) {
				http.NotFound(w, r)
				return
			}
			s.erreurServeur(w, r, errLecture)
			return
		}
		s.rendrePagePromotion(w, r, sc, messageUtilisateur(err))
		return
	}

	// Navigation de page complète classique (pas htmx) : redirection HTTP
	// standard vers la liste, comme demandé — la promotion est une décision
	// engageante, pas un swap discret dans un tableau.
	http.Redirect(w, r, "/scenarios", http.StatusSeeOther)
}
