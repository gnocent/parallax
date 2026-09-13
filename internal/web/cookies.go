package web

import (
	"net/http"

	"parallax/internal/auth"
)

// nomCookie est le cookie de session. HttpOnly (inaccessible en JS, donc à un
// script injecté par XSS) ; SameSite=Lax limite l'envoi sur navigation
// cross-site ; Secure seulement quand la requête est déjà en TLS, pour ne pas
// casser un accès interne en HTTP simple derrière un pare-feu.
const nomCookie = "parallax_session"

// CookiesSecurises force l'attribut Secure même quand la requête arrive en
// HTTP simple — le cas d'un reverse proxy qui termine le TLS devant Parallax
// (revue 2026-09-12). Posé par cmd/parallax depuis PARALLAX_COOKIES_SECURE.
var CookiesSecurises bool

func cookieSecurise(r *http.Request) bool { return CookiesSecurises || r.TLS != nil }

func poserCookieSession(w http.ResponseWriter, r *http.Request, s auth.Session) {
	http.SetCookie(w, &http.Cookie{
		Name:     nomCookie,
		Value:    s.Jeton,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   cookieSecurise(r),
		Expires:  s.ExpireLe,
	})
}

func effacerCookieSession(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     nomCookie,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   cookieSecurise(r),
		MaxAge:   -1,
	})
}
