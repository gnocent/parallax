// Package web est la couche HTTP de Parallax : net/http et le ServeMux de
// Go 1.22 (aucun routeur tiers), rendu serveur via html/template, HTMX
// embarqué dans le binaire.
//
// Convention d'écran, à respecter dans tout nouveau fichier handlers_*.go :
// une fonction routesXxx() enregistrée depuis routes() (server.go — jamais
// modifiée par un écran, l'intégration ajoute l'appel), et pour chaque entité
// le patron d'édition en ligne HTMX :
//
//	GET  /xxx                  page complète (liste + formulaire de création)
//	POST /xxx                  créer, répond la ligne créée (fragment)
//	GET  /xxx/{id}              ligne en lecture (fragment, sert au "annuler")
//	GET  /xxx/{id}/editer        ligne en édition (fragment)
//	PUT  /xxx/{id}              modifier, répond la ligne en lecture (fragment)
//	POST /xxx/{id}/archiver      bascule d'activité, répond la ligne (fragment)
//	POST /xxx/{id}/reactiver
//
// s.lecteur(h) protège une lecture (toute personne connectée) ;
// s.editeur(h) protège une écriture (EDITEUR ou ADMIN, CSRF vérifié) ;
// s.administrateur(h) réserve à ADMIN (gestion des comptes).
package web

import (
	"crypto/subtle"
	"log"
	"net/http"
	"net/url"
	"time"

	"parallax/internal/depot"
)

// chargerSession lit le cookie de session sur chaque requête et, s'il est
// valide, pose l'identité et la session dans le contexte. Ne bloque jamais :
// une session absente ou invalide laisse simplement la requête anonyme, à
// exigerAuth de refuser l'accès aux routes qui le demandent.
func (s *serveur) chargerSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(nomCookie)
		if err != nil || cookie.Value == "" {
			next.ServeHTTP(w, r)
			return
		}
		session, identite, err := s.auth.Courant(cookie.Value)
		if err != nil {
			effacerCookieSession(w, r)
			next.ServeHTTP(w, r)
			return
		}
		poserCookieSession(w, r, session) // reflète la prolongation glissante
		ctx := avecSession(avecIdentite(r.Context(), identite), session)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// estRequeteHTMX indique si la requête vient d'une interaction htmx (par
// opposition à une navigation de page complète) : ça change comment on répond
// à un refus d'accès (redirection côté client via HX-Redirect plutôt qu'une
// redirection HTTP classique, que htmx ne suit pas en swap).
func estRequeteHTMX(r *http.Request) bool { return r.Header.Get("HX-Request") == "true" }

// exigerAuth refuse l'accès à qui n'a pas de session valide.
func (s *serveur) exigerAuth(suivant http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := identiteDepuis(r.Context()); !ok {
			if estRequeteHTMX(r) {
				w.Header().Set("HX-Redirect", "/connexion")
				w.WriteHeader(http.StatusOK)
				return
			}
			http.Redirect(w, r, "/connexion?suite="+url.QueryEscape(r.URL.Path), http.StatusSeeOther)
			return
		}
		suivant(w, r)
	}
}

// exigerRole enchaîne exigerAuth puis vérifie que le rôle de l'utilisateur
// figure parmi ceux autorisés.
func (s *serveur) exigerRole(roles ...string) func(http.HandlerFunc) http.HandlerFunc {
	return func(suivant http.HandlerFunc) http.HandlerFunc {
		return s.exigerAuth(func(w http.ResponseWriter, r *http.Request) {
			identite, _ := identiteDepuis(r.Context())
			for _, role := range roles {
				if identite.Role == role {
					suivant(w, r)
					return
				}
			}
			http.Error(w, "accès refusé pour votre rôle", http.StatusForbidden)
		})
	}
}

// verifierCSRF exige un jeton synchronizer (en-tête X-CSRF-Token, posé
// globalement par htmx via hx-headers sur <body>, ou champ de formulaire
// _csrf) égal à celui de la session, comparé en temps constant.
//
// N'agit que sur les méthodes qui modifient l'état (ni GET, ni HEAD, ni
// OPTIONS) : c'est ce qui permet à s.editeur et s.administrateur de protéger
// indifféremment une route de lecture ou d'écriture d'un même groupe sans
// que la lecture n'exige un jeton qu'un simple lien ou une navigation directe
// n'a aucune raison de porter.
func (s *serveur) verifierCSRF(suivant http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
			suivant(w, r)
			return
		}
		session, ok := sessionDepuis(r.Context())
		if !ok {
			http.Error(w, "session requise", http.StatusForbidden)
			return
		}
		jeton := r.Header.Get("X-CSRF-Token")
		if jeton == "" {
			jeton = r.FormValue("_csrf")
		}
		if subtle.ConstantTimeCompare([]byte(jeton), []byte(session.CSRFToken)) != 1 {
			http.Error(w, "jeton CSRF invalide ou absent", http.StatusForbidden)
			return
		}
		suivant(w, r)
	}
}

// lecteur protège un écran de consultation : toute personne connectée.
func (s *serveur) lecteur(h http.HandlerFunc) http.HandlerFunc {
	return s.exigerAuth(h)
}

// editeur protège une écriture : rôle EDITEUR ou ADMIN, jeton CSRF vérifié.
func (s *serveur) editeur(h http.HandlerFunc) http.HandlerFunc {
	return s.exigerRole(depot.RoleEditeur, depot.RoleAdmin)(s.verifierCSRF(h))
}

// administrateur protège la gestion des comptes : rôle ADMIN seul.
func (s *serveur) administrateur(h http.HandlerFunc) http.HandlerFunc {
	return s.exigerRole(depot.RoleAdmin)(s.verifierCSRF(h))
}

// journaliser trace méthode, chemin et durée de chaque requête sur la sortie
// standard du binaire. Volontairement minimal : ce n'est pas le journal
// applicatif de la table journal, seulement l'observabilité du serveur.
func journaliser(suivant http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		debut := time.Now()
		suivant.ServeHTTP(w, r)
		log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(debut).Round(time.Millisecond))
	})
}

// recuperer transforme une panique de handler en 500 au lieu de faire tomber
// le serveur — un seul écran défaillant ne doit pas priver les autres
// utilisateurs de l'application.
func recuperer(suivant http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if p := recover(); p != nil {
				log.Printf("panique sur %s %s : %v", r.Method, r.URL.Path, p)
				http.Error(w, "erreur interne", http.StatusInternalServerError)
			}
		}()
		suivant.ServeHTTP(w, r)
	})
}

// pasDeCache tient les fragments htmx hors du cache navigateur : une ligne
// archivée puis rechargée en arrière ne doit pas réapparaître active.
func pasDeCache(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
}
