package web

import (
	"net/http"
	"strconv"
	"strings"

	"parallax/internal/depot"
)

// Journal des modifications (backlog v3.4) : une page globale filtrable,
// réservée aux administrateurs, et un fragment « historique » que chaque
// fiche charge en htmx pour sa propre entité. Le journal se lit, il ne
// s'annule pas (décision 2026-09-12).
func (s *serveur) routesJournal() {
	s.mux.HandleFunc("GET /journal", s.administrateur(s.journalPage))
	s.mux.HandleFunc("POST /journal/purger", s.administrateur(s.journalPurger))
	s.mux.HandleFunc("GET /journal/historique/{entite}/{id}", s.lecteur(s.journalHistorique))
}

// depotPour renvoie le dépôt portant l'identité connectée, pour que les
// écritures soient journalisées à son nom. Sans identité (ne devrait pas
// arriver derrière s.lecteur), le dépôt nu — l'entrée est écrite sans auteur
// plutôt que refusée.
func (s *serveur) depotPour(r *http.Request) *depot.Depot {
	if identite, ok := identiteDepuis(r.Context()); ok {
		return s.depot.Au(identite.UtilisateurID)
	}
	return s.depot
}

// filtreJournalDepuisRequete lit les filtres de la page : entité, action,
// auteur, période. Une valeur illisible est ignorée, jamais une erreur.
func filtreJournalDepuisRequete(r *http.Request) depot.FiltreJournal {
	f := depot.FiltreJournal{
		Entite: strings.TrimSpace(r.URL.Query().Get("entite")),
		Action: strings.TrimSpace(r.URL.Query().Get("action")),
		Depuis: strings.TrimSpace(r.URL.Query().Get("depuis")),
		Jusqua: strings.TrimSpace(r.URL.Query().Get("jusqua")),
	}
	if depot.ValiderDate("depuis", f.Depuis) != nil {
		f.Depuis = ""
	}
	if depot.ValiderDate("jusqua", f.Jusqua) != nil {
		f.Jusqua = ""
	}
	if v, err := strconv.ParseInt(r.URL.Query().Get("utilisateur"), 10, 64); err == nil && v > 0 {
		f.UtilisateurID = &v
	}
	return f
}

// entreeJournalAffichable aplatit une entrée pour le gabarit : le JSON
// avant/après est montré tel quel dans un <details>, lisible sans outil.
type entreeJournalAffichable struct {
	depot.EntreeJournal
	Auteur string
	Avant  string
	Apres  string
}

func entreesAffichables(entrees []depot.EntreeJournal) []entreeJournalAffichable {
	out := make([]entreeJournalAffichable, len(entrees))
	for i, e := range entrees {
		a := entreeJournalAffichable{EntreeJournal: e, Auteur: "—"}
		if e.UtilisateurLogin != nil {
			a.Auteur = *e.UtilisateurLogin
		}
		if e.Avant != nil {
			a.Avant = *e.Avant
		}
		if e.Apres != nil {
			a.Apres = *e.Apres
		}
		out[i] = a
	}
	return out
}

func (s *serveur) journalPage(w http.ResponseWriter, r *http.Request) {
	filtre := filtreJournalDepuisRequete(r)
	entrees, err := s.depot.ListerJournal(filtre)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	if s.exporter(w, r, func() (tableau, error) {
		t := tableau{Titre: "Journal", Colonnes: []string{"Horodatage", "Auteur", "Entité", "Identifiant", "Action", "Avant", "Après"}}
		for _, e := range entreesAffichables(entrees) {
			t.Lignes = append(t.Lignes, []any{e.Horodatage, e.Auteur, e.Entite, e.EntiteID, e.Action, e.Avant, e.Apres})
		}
		return t, nil
	}) {
		return
	}
	utilisateurs, err := s.depot.ListerUtilisateurs(true)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	var utilisateurFiltre int64
	if filtre.UtilisateurID != nil {
		utilisateurFiltre = *filtre.UtilisateurID
	}
	// le formulaire de purge est un <form> classique : jeton CSRF en champ
	// caché, comme vue_ouverte_page.
	var csrfToken string
	if session, ok := sessionDepuis(r.Context()); ok {
		csrfToken = session.CSRFToken
	}
	s.rendrePage(w, r, s.titre(r, "titre.journal"), "journal_page", map[string]any{
		"Entrees":             entreesAffichables(entrees),
		"Filtre":              filtre,
		"UtilisateurFiltreID": utilisateurFiltre,
		"Entites":             depot.EntitesJournal,
		"Actions":             []string{depot.ActionCreation, depot.ActionModification, depot.ActionCorrection, depot.ActionSuppression},
		"Utilisateurs":        utilisateurs,
		"Export":              liensExport(r),
		"RetentionJours":      int(depot.RetentionJournal.Hours() / 24),
		"Message":             strings.TrimSpace(r.URL.Query().Get("message")),
		"CSRFToken":           csrfToken,
	})
}

// journalPurger supprime les entrées au-delà de la rétention, à la demande.
func (s *serveur) journalPurger(w http.ResponseWriter, r *http.Request) {
	n, err := s.depot.PurgerJournal(depot.RetentionJournal)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	http.Redirect(w, r, "/journal?message="+strconv.FormatInt(n, 10)+"+entr%C3%A9e(s)+purg%C3%A9e(s)", http.StatusSeeOther)
}

// journalHistorique est le fragment « historique » d'une fiche : les
// entrées d'une entité, chargées en htmx par
// <div hx-get="/journal/historique/serveur/42" hx-trigger="load">.
func (s *serveur) journalHistorique(w http.ResponseWriter, r *http.Request) {
	entite := r.PathValue("entite")
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 || !entiteJournalConnue(entite) {
		http.Error(w, "entité ou identifiant invalide", http.StatusBadRequest)
		return
	}
	// les comptes et les paramètres n'ont de fiche que pour un administrateur :
	// leur historique aussi (revue 2026-09-12).
	if entite == "utilisateur" || entite == "parametre" {
		if identite, ok := identiteDepuis(r.Context()); !ok || identite.Role != depot.RoleAdmin {
			http.Error(w, "réservé aux administrateurs", http.StatusForbidden)
			return
		}
	}
	entrees, err := s.depot.HistoriqueEntite(entite, id)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	s.rendreFragment(w, r, "journal_historique", map[string]any{
		"Entite": entite, "EntiteID": id, "Entrees": entreesAffichables(entrees),
	})
}

func entiteJournalConnue(e string) bool {
	for _, c := range depot.EntitesJournal {
		if c == e {
			return true
		}
	}
	return false
}
