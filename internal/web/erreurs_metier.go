package web

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"parallax/internal/depot"
)

// erreurMetier distingue une erreur « attendue » du domaine (à afficher à
// l'utilisateur dans le fragment renvoyé) d'une erreur inattendue (à
// journaliser et à traduire en 500). Tout appel à un dépôt qui peut échouer
// pour une raison métier doit tester ce booléen avant de choisir entre
// s.erreurServeur et un message affiché.
func erreurMetier(err error) bool {
	if err == nil {
		return false
	}
	for _, cible := range []error{
		depot.ErrValidation, depot.ErrConflit, depot.ErrReference,
		depot.ErrChevauchement, depot.ErrImmuable, depot.ErrInvariant,
		depot.ErrIntrouvable,
	} {
		if errors.Is(err, cible) {
			return true
		}
	}
	return false
}

// messageUtilisateur traduit une erreur métier en phrase affichable. Pour une
// erreur non métier, ce message ne doit jamais atteindre l'utilisateur —
// appeler s.erreurServeur à la place.
func messageUtilisateur(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, depot.ErrConflit):
		return "Ce code existe déjà."
	case errors.Is(err, depot.ErrIntrouvable):
		return "Élément introuvable : a-t-il été modifié entretemps par quelqu'un d'autre ?"
	case errors.Is(err, depot.ErrValidation),
		errors.Is(err, depot.ErrReference),
		errors.Is(err, depot.ErrImmuable),
		errors.Is(err, depot.ErrInvariant),
		errors.Is(err, depot.ErrChevauchement):
		return err.Error()
	default:
		return "Erreur : " + err.Error()
	}
}

// inclureInactifs lit la case à cocher « inclure les archivés », commune à
// tous les écrans de référentiel. Fonctionne aussi bien en GET (query) qu'en
// POST/PUT (corps) : r.FormValue lit les deux.
func inclureInactifs(r *http.Request) bool {
	return r.FormValue("inclure_inactifs") != ""
}

// idChemin lit et parse le paramètre {id} du chemin de la requête.
func idChemin(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		return 0, errors.New("identifiant invalide dans l'adresse")
	}
	return id, nil
}

// repondreIntrouvable répond à un ErrIntrouvable survenu après une mutation
// réussie (course avec un autre utilisateur : la ligne a disparu entre
// l'action et la relecture — l'application ne supprime jamais rien, donc ce
// cas est rare, mais un écran ne doit pas planter dessus). Le colspan large
// est une tolérance volontaire : cette ligne de repli n'a pas besoin de
// s'aligner avec les colonnes exactes de chaque tableau.
//
// Pour toute autre erreur, se comporte comme s.erreurServeur.
func (s *serveur) repondreIntrouvable(w http.ResponseWriter, r *http.Request, err error) {
	if !errors.Is(err, depot.ErrIntrouvable) {
		s.erreurServeur(w, r, err)
		return
	}
	pasDeCache(w)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, `<tr><td colspan="12" class="message-erreur">`+
		`Élément introuvable — recharger la page.</td></tr>`)
}
