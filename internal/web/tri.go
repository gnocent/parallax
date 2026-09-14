package web

import (
	"net/http"
	"net/url"
	"sort"
)

// tri.go — tri d'un tableau à axes par une colonne de valeurs, au clic sur
// son en-tête (vues, comparaison, capacité). Le tri respecte la hiérarchie :
// les lignes sont ordonnées par valeur à l'intérieur de leur groupe parent
// (toutes les valeurs d'axes sauf la dernière), jamais à travers — c'est le
// tri « par valeur » d'un tableau croisé, et la fusion verticale des axes
// reste lisible. Avec un seul axe, c'est un tri complet.
//
// Convention d'URL : "tri" (identifiant de colonne, propre à chaque écran)
// et "sens" (asc, ou desc). Les axes ne se trient pas (ils sont l'ordre).

// ordreTri renvoie la permutation des lignes triées par valeur dans leur
// groupe parent. Stable : à valeur égale, l'ordre des clés est conservé.
func ordreTri(cles [][]string, valeur func(i int) float64, desc bool) []int {
	idx := make([]int, len(cles))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(a, b int) bool {
		ca, cb := cles[idx[a]], cles[idx[b]]
		for k := 0; k+1 < len(ca) && k+1 < len(cb); k++ {
			if ca[k] != cb[k] {
				return ca[k] < cb[k]
			}
		}
		va, vb := valeur(idx[a]), valeur(idx[b])
		if desc {
			return va > vb
		}
		return va < vb
	})
	return idx
}

// enteteTri est un en-tête de colonne cliquable : son identifiant de tri,
// son libellé, s'il porte le tri courant et dans quel sens, et le sens que
// le prochain clic demanderait (asc → desc → asc). URL n'est renseignée
// que par les écrans en navigation GET classique (capacité) ; les écrans
// htmx construisent leur hx-get depuis Code et Suivant.
type enteteTri struct {
	Code    string
	Libelle string
	Actif   bool
	Desc    bool
	Suivant string
	URL     string
}

func nouvelEnteteTri(code, libelle, tri string, desc bool) enteteTri {
	e := enteteTri{Code: code, Libelle: libelle, Actif: tri == code, Desc: tri == code && desc, Suivant: "asc"}
	if e.Actif && !desc {
		e.Suivant = "desc"
	}
	return e
}

func triDepuisRequete(r *http.Request) (tri string, desc bool) {
	return r.FormValue("tri"), r.FormValue("sens") == "desc"
}

// lienTri : l'URL de la page courante avec le tri demandé, pour un en-tête
// en navigation classique.
func lienTri(r *http.Request, e enteteTri) string {
	q := url.Values{}
	for k, v := range r.URL.Query() {
		if k != "tri" && k != "sens" && k != "export" {
			q[k] = v
		}
	}
	q.Set("tri", e.Code)
	q.Set("sens", e.Suivant)
	return r.URL.Path + "?" + q.Encode()
}
