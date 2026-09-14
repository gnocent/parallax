package web

import "parallax/internal/vues"

// maxAxes est le nombre d'axes de regroupement qu'un écran accepte (axe1 …
// axe6) — vues, comparaison, capacité.
const maxAxes = 6

// selecteurEtiquettes est le contexte du gabarit "selecteur_axes"
// (templates/composants/selecteur_axes.html) : des étiquettes qu'on clique
// ou qu'on glisse dans une zone, réordonnables, qui alimentent des champs
// cachés (static/axes.js). Deux usages, même mécanique : les axes de
// regroupement (champs axe1 … axeN, Champ vide) et les colonnes affichées
// (un champ multi-valeurs « colonne », dans l'ordre des étiquettes — qui
// devient l'ordre des colonnes). Le formulaire reste le même qu'avec des
// listes déroulantes ou des cases à cocher : les exports, les vues
// enregistrées et les liens préréglés n'y voient aucune différence.
type selecteurEtiquettes struct {
	Choisis     []optionDimension // dans l'ordre
	Disponibles []optionDimension
	Max         int
	Champ       string // nom du champ multi-valeurs ; vide : axe1 … axeN
	CleVide     string // clé i18n du texte de la zone vide
}

func nouveauSelecteurEtiquettes(options []optionDimension, choisis []string, champ string, max int, cleVide string) selecteurEtiquettes {
	s := selecteurEtiquettes{Max: max, Champ: champ, CleVide: cleVide}
	pris := map[string]bool{}
	for _, code := range choisis {
		for _, d := range options {
			if d.Code == code && !pris[code] {
				s.Choisis = append(s.Choisis, d)
				pris[code] = true
			}
		}
	}
	for _, d := range options {
		if !pris[d.Code] {
			s.Disponibles = append(s.Disponibles, d)
		}
	}
	return s
}

func nouveauSelecteurAxes(dimensions []optionDimension, choisis []string) selecteurEtiquettes {
	return nouveauSelecteurEtiquettes(dimensions, choisis, "", maxAxes, "commun.axes_vide")
}

// colonnesParDefaut : les colonnes proposées d'emblée dans le constructeur
// de vues et la comparaison — le tableau croisé usuel (serveurs, cœurs, RAM,
// disques, nœuds), les autres restant à portée d'un clic.
var colonnesParDefaut = []vues.Colonne{
	vues.ColNbServeurs, "cpu", "ram", "hdd", "ssd", vues.ColNbNoeuds,
}

// nouveauSelecteurColonnes : choisies vides → colonnesParDefaut.
func nouveauSelecteurColonnes(choisies []vues.Colonne) selecteurEtiquettes {
	if len(choisies) == 0 {
		choisies = colonnesParDefaut
	}
	codes := make([]string, len(choisies))
	for i, c := range choisies {
		codes[i] = string(c)
	}
	return nouveauSelecteurEtiquettes(colonnesAffichables(), codes, "colonne", len(vues.Colonnes), "commun.colonnes_vide")
}
