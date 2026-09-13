package web

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// aide_formulaire.go regroupe des lectures de formulaire partagées par
// plusieurs écrans (clusters, serveurs) : un identifiant facultatif issu d'un
// <select> avec option vide (tier, usage, zone…), un identifiant
// obligatoire, un texte facultatif normalisé en pointeur nil si vide — la
// convention des dépôts, où l'absence de valeur documentaire est un NULL, pas
// une chaîne vide.

// idOptionnel lit un champ de formulaire censé contenir un identifiant entier
// facultatif : chaîne vide -> nil, sinon l'entier parsé.
func idOptionnel(r *http.Request, nom string) (*int64, error) {
	v := strings.TrimSpace(r.FormValue(nom))
	if v == "" {
		return nil, nil
	}
	id, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("%s invalide : « %s »", nom, v)
	}
	return &id, nil
}

// idRequis lit un champ de formulaire censé contenir un identifiant entier
// obligatoire. Une valeur vide ou non numérique renvoie 0 plutôt qu'une
// erreur Go : les dépôts refusent déjà un id <= 0 avec ErrValidation (voir
// validerCluster, CreerServeur…), ce qui redescend comme une erreur métier
// affichée dans le fragment plutôt qu'une 400 brute — cohérent avec la règle
// « jamais 4xx/5xx sur une erreur métier d'un endpoint htmx ».
func idRequis(r *http.Request, nom string) int64 {
	id, err := strconv.ParseInt(strings.TrimSpace(r.FormValue(nom)), 10, 64)
	if err != nil {
		return 0
	}
	return id
}

// texteOptionnel lit un champ texte et le normalise en pointeur : chaîne vide
// ou uniquement blanche -> nil, sinon la valeur recadrée.
func texteOptionnel(r *http.Request, nom string) *string {
	v := strings.TrimSpace(r.FormValue(nom))
	if v == "" {
		return nil
	}
	return &v
}
