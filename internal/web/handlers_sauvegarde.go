package web

import (
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"parallax/internal/depot"
	"parallax/internal/sauvegarde"
)

// routesSauvegarde enregistre l'écran de paramètres de la sauvegarde locale
// automatique (backlog v3.7), réservé à un administrateur. Le dépôt
// périodique sur S3 (cmd/parallax/main.go, PARALLAX_S3_*) reste un mécanisme
// indépendant, réglé par variables d'environnement : celui-ci écrit sur le
// disque de la machine qui héberge Parallax, tous les jours à heure fixe, et
// seulement si quelque chose a changé (depot.ActiviteDepuis) — voir
// planifierSauvegardeLocale dans cmd/parallax.
//
//	GET  /parametres/sauvegarde            formulaire, état actuel, fichiers
//	POST /parametres/sauvegarde            valide puis enregistre les réglages
//	POST /parametres/sauvegarde/maintenant déclenche une sauvegarde immédiate
func (s *serveur) routesSauvegarde() {
	s.mux.HandleFunc("GET /parametres/sauvegarde", s.administrateur(s.parametreSauvegardePage))
	s.mux.HandleFunc("POST /parametres/sauvegarde", s.administrateur(s.parametreSauvegardeEnregistrer))
	s.mux.HandleFunc("POST /parametres/sauvegarde/maintenant", s.administrateur(s.parametreSauvegardeMaintenant))
}

func (s *serveur) parametreSauvegardePage(w http.ResponseWriter, r *http.Request) {
	dossier, _, err := s.depot.LireParametre(depot.ParametreSauvegardeDossier)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	heure, _, err := s.depot.LireParametre(depot.ParametreSauvegardeHeure)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	retention, _, err := s.depot.LireParametre(depot.ParametreSauvegardeRetentionJours)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	s.rendreSauvegardePage(w, r, dossier, heure, retention, "", "")
}

// validerReglagesSauvegarde renvoie un message d'erreur métier, vide si tout
// est valide — même patron que CompilerGabarit (parametres_gabarit.go) :
// jamais enregistré si invalide, jamais une 4xx/5xx pour une erreur de
// saisie.
func validerReglagesSauvegarde(dossier, heure, retentionTexte string) string {
	if dossier == "" {
		return "Le dossier de destination est obligatoire."
	}
	if _, err := time.Parse("15:04", heure); err != nil {
		return "Heure invalide : format HH:MM attendu (ex. 02:00)."
	}
	retention, err := strconv.Atoi(retentionTexte)
	if err != nil || retention < 1 {
		return "La rétention doit être un nombre entier de jours, au moins 1."
	}
	return ""
}

func (s *serveur) parametreSauvegardeEnregistrer(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "formulaire illisible", http.StatusBadRequest)
		return
	}
	dossier := strings.TrimSpace(r.FormValue("dossier"))
	heure := strings.TrimSpace(r.FormValue("heure"))
	retention := strings.TrimSpace(r.FormValue("retention_jours"))

	if erreur := validerReglagesSauvegarde(dossier, heure, retention); erreur != "" {
		s.rendreSauvegardePage(w, r, dossier, heure, retention, erreur, "")
		return
	}
	// Validé à la saisie, comme un gabarit qui ne compile pas : un dossier
	// inaccessible en écriture n'est jamais enregistré, plutôt que découvert
	// au premier passage du planificateur, cette nuit.
	if err := os.MkdirAll(dossier, 0o750); err != nil {
		s.rendreSauvegardePage(w, r, dossier, heure, retention,
			"Dossier inaccessible en écriture : "+err.Error(), "")
		return
	}

	var auteur *int64
	if identite := identiteOuNil(r); identite != nil {
		auteur = &identite.UtilisateurID
	}
	dépôt := s.depotPour(r)
	if err := dépôt.DefinirParametre(depot.ParametreSauvegardeDossier, dossier, auteur); err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	if err := dépôt.DefinirParametre(depot.ParametreSauvegardeHeure, heure, auteur); err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	if err := dépôt.DefinirParametre(depot.ParametreSauvegardeRetentionJours, retention, auteur); err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	s.rendreSauvegardePage(w, r, dossier, heure, retention, "", "Réglages enregistrés.")
}

// parametreSauvegardeMaintenant écrit une sauvegarde immédiatement dans le
// dossier déjà enregistré — pratique pour vérifier que le chemin choisi
// fonctionne réellement sans attendre l'heure programmée. Inconditionnelle :
// contrairement au planificateur, un geste explicite de l'administrateur
// n'a pas à se justifier par un changement récent.
func (s *serveur) parametreSauvegardeMaintenant(w http.ResponseWriter, r *http.Request) {
	dossier, present, err := s.depot.LireParametre(depot.ParametreSauvegardeDossier)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	heure, _, _ := s.depot.LireParametre(depot.ParametreSauvegardeHeure)
	retention, _, _ := s.depot.LireParametre(depot.ParametreSauvegardeRetentionJours)
	if !present || dossier == "" {
		s.rendreSauvegardePage(w, r, dossier, heure, retention,
			"Renseignez et enregistrez d'abord un dossier de destination.", "")
		return
	}

	nom, err := sauvegarde.EcrireLocale(s.depot.Base(), dossier)
	if err != nil {
		s.rendreSauvegardePage(w, r, dossier, heure, retention, "Échec de la sauvegarde : "+err.Error(), "")
		return
	}
	var auteur *int64
	if identite := identiteOuNil(r); identite != nil {
		auteur = &identite.UtilisateurID
	}
	if err := s.depotPour(r).DefinirParametre(depot.ParametreSauvegardeDerniereReussite, time.Now().Format(time.RFC3339), auteur); err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	s.rendreSauvegardePage(w, r, dossier, heure, retention, "", "Sauvegarde écrite : "+nom+".")
}

// rendreSauvegardePage assemble le contexte de l'écran : réglages tels que
// saisis (jamais relus en base après une erreur, pour ne pas effacer la
// frappe de l'administrateur), dernière réussite, et le contenu actuel du
// dossier — pour vérifier que la rétention fait bien son travail sans avoir
// à s'y connecter en SSH.
func (s *serveur) rendreSauvegardePage(w http.ResponseWriter, r *http.Request, dossier, heure, retention, erreur, succes string) {
	derniereReussite, _, err := s.depot.LireParametre(depot.ParametreSauvegardeDerniereReussite)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	var fichiers []sauvegarde.FichierLocal
	if dossier != "" {
		fichiers, _ = sauvegarde.ListerLocales(dossier) // dossier pas encore créé : liste vide, pas une erreur affichée
	}
	csrf := ""
	if session, ok := sessionDepuis(r.Context()); ok {
		csrf = session.CSRFToken
	}
	s.rendrePage(w, r, s.titre(r, "titre.parametres_sauvegarde"), "parametres_sauvegarde_page", map[string]any{
		"Dossier":          dossier,
		"Heure":            heure,
		"RetentionJours":   retention,
		"Active":           dossier != "" && heure != "",
		"DerniereReussite": derniereReussite,
		"Fichiers":         fichiers,
		"Erreur":           erreur,
		"Succes":           succes,
		"CSRFToken":        csrf,
	})
}
