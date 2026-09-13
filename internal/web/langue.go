package web

import (
	"net/http"
	"net/url"
	"time"

	"parallax/internal/i18n"
)

// Bascule de langue de l'interface (français par défaut, anglais en
// option — voir docs/guide-utilisateur.md). Une préférence cosmétique, pas
// une donnée métier : portée par un simple cookie, jamais par le compte
// (pas de migration, aucun impact sur les autres utilisateurs).

const cookieLangue = "parallax_langue"

// routesLangue enregistre le lien de bascule du menu. GET volontairement :
// changer la langue d'affichage n'est pas une écriture de donnée métier, au
// même titre que choisir un scénario dans un sélecteur — pas besoin du
// jeton CSRF que les vraies mutations exigent.
func (s *serveur) routesLangue() {
	s.mux.HandleFunc("GET /langue/{lang}", s.langueBasculer)
}

// langueDepuisRequete lit la préférence de langue de la requête courante.
// Cookie absent ou invalide : i18n.Defaut, jamais une erreur — un écran de
// lecture ne doit jamais tomber pour un cookie mal formé.
func langueDepuisRequete(r *http.Request) i18n.Lang {
	cookie, err := r.Cookie(cookieLangue)
	if err != nil {
		return i18n.Defaut
	}
	lang, _ := i18n.EstValide(cookie.Value)
	return lang
}

// pageDeRetour extrait un chemin local sûr du Referer envoyé par le
// navigateur (une URL complète, "http://hôte/écran") : ne renvoyer que
// Path+RawQuery, jamais l'URL telle quelle — un Referer absent ou illisible
// retombe sur l'accueil, plutôt que de rediriger vers une origine non
// maîtrisée.
func pageDeRetour(referer string) string {
	u, err := url.Parse(referer)
	if err != nil || u.Path == "" {
		return "/"
	}
	chemin := u.Path
	if u.RawQuery != "" {
		chemin += "?" + u.RawQuery
	}
	return chemin
}

func (s *serveur) langueBasculer(w http.ResponseWriter, r *http.Request) {
	lang, _ := i18n.EstValide(r.PathValue("lang"))
	http.SetCookie(w, &http.Cookie{
		Name:     cookieLangue,
		Value:    string(lang),
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   cookieSecurise(r),
		Expires:  time.Now().AddDate(1, 0, 0),
	})
	http.Redirect(w, r, pageDeRetour(r.Referer()), http.StatusSeeOther)
}
