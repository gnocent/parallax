package web

import (
	"errors"
	"log"
	"net/http"
	"sync"
	"time"

	"parallax/internal/auth"
)

// connexionFormulaire affiche l'écran de connexion. suite (le chemin d'où
// l'utilisateur a été redirigé) est reporté dans le formulaire pour y
// retourner après connexion.
func (s *serveur) connexionFormulaire(w http.ResponseWriter, r *http.Request) {
	if _, ok := identiteDepuis(r.Context()); ok {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	s.rendrePage(w, r, s.titre(r, "titre.connexion"), "connexion_page", map[string]any{
		"Suite": r.URL.Query().Get("suite"),
	})
}

// connexionSoumettre vérifie les identifiants et ouvre une session. Pas de
// vérification CSRF ici : il n'existe encore aucune session à laquelle
// comparer un jeton. Le coût résiduel (CSRF de connexion, imposer une session
// à la victime) est mineur et déjà atténué par SameSite=Lax sur le cookie.
func (s *serveur) connexionSoumettre(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "formulaire illisible", http.StatusBadRequest)
		return
	}
	login := r.FormValue("login")
	motDePasse := r.FormValue("mot_de_passe")
	suite := r.FormValue("suite")

	if !freinConnexion.autorise(login) {
		// trop d'échecs récents sur ce login : on refuse sans même vérifier,
		// le message reste celui d'un identifiant invalide.
		log.Printf("connexion refusée (frein) login=%q depuis %s", login, r.RemoteAddr)
		w.WriteHeader(http.StatusTooManyRequests)
		s.rendrePage(w, r, s.titre(r, "titre.connexion"), "connexion_page", map[string]any{
			"Erreur": "Trop de tentatives, réessayez dans quelques minutes.", "Suite": suite, "Login": login,
		})
		return
	}
	session, _, err := s.auth.Connecter(login, motDePasse)
	if err != nil {
		messageErreur := "Identifiants invalides."
		if errors.Is(err, auth.ErrCompteInactif) {
			messageErreur = "Ce compte est désactivé."
		} else if !errors.Is(err, auth.ErrIdentifiantsInvalides) {
			s.erreurServeur(w, r, err)
			return
		}
		// un échec est journalisé (login, adresse) pour l'exploitation, et
		// ralenti : un essai en force n'a plus de raison d'aller vite.
		log.Printf("connexion échouée login=%q depuis %s : %v", login, r.RemoteAddr, err)
		freinConnexion.echec(login)
		time.Sleep(delaiEchecConnexion)
		w.WriteHeader(http.StatusUnauthorized)
		s.rendrePage(w, r, s.titre(r, "titre.connexion"), "connexion_page", map[string]any{
			"Erreur": messageErreur, "Suite": suite, "Login": login,
		})
		return
	}

	freinConnexion.succes(login)
	poserCookieSession(w, r, session)
	if suite == "" || suite[0] != '/' {
		suite = "/"
	}
	http.Redirect(w, r, suite, http.StatusSeeOther)
}

// Frein sur les échecs de connexion (revue 2026-09-12) : au-delà de
// seuilEchecsConnexion échecs sur un même login dans la fenêtre, le login
// est refusé pendant la fenêtre. En mémoire, par login — suffisant pour une
// application interne à quelques dizaines de comptes, et sans état en base.
const (
	seuilEchecsConnexion  = 8
	fenetreFreinConnexion = 10 * time.Minute
	delaiEchecConnexion   = 400 * time.Millisecond
)

type freinConnexions struct {
	mu     sync.Mutex
	echecs map[string][]time.Time
}

var freinConnexion = &freinConnexions{echecs: map[string][]time.Time{}}

func (f *freinConnexions) recents(login string, maintenant time.Time) []time.Time {
	var out []time.Time
	for _, t := range f.echecs[login] {
		if maintenant.Sub(t) < fenetreFreinConnexion {
			out = append(out, t)
		}
	}
	return out
}

func (f *freinConnexions) autorise(login string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	recents := f.recents(login, time.Now())
	f.echecs[login] = recents
	return len(recents) < seuilEchecsConnexion
}

func (f *freinConnexions) echec(login string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	maintenant := time.Now()
	f.echecs[login] = append(f.recents(login, maintenant), maintenant)
}

func (f *freinConnexions) succes(login string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.echecs, login)
}

// deconnexionSoumettre invalide la session courante et efface le cookie.
func (s *serveur) deconnexionSoumettre(w http.ResponseWriter, r *http.Request) {
	if session, ok := sessionDepuis(r.Context()); ok {
		if err := s.auth.Deconnecter(session.Jeton); err != nil {
			s.erreurServeur(w, r, err)
			return
		}
	}
	effacerCookieSession(w, r)
	http.Redirect(w, r, "/connexion", http.StatusSeeOther)
}
