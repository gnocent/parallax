package vues

import (
	"math"

	"parallax/internal/depot"
)

// licences.go : les colonnes de licences du constructeur de vues (backlog
// v3.3, docs/modele-donnees.md §8.2). Les licences ne sont pas une règle de
// capacité par cluster mais une colonne calculée sur le groupe de la vue,
// techno par techno, avec le contrat en vigueur pour l'année de la vue.
//
// Tableau de référence (§8.2), pour un groupe et une techno :
//
//	mécanisme       | MACHINE                                  | CLUSTER                                    | GLOBAL
//	NOEUDS          | Σ nb_noeuds                              | idem                                       | idem
//	MAX_NOEUDS_RAM  | Σ max(nb, ceil(ram / ram_max)) / serveur | Σ max(Σnb, ceil(Σram / ram_max)) / cluster | max(Σnb, ceil(Σram / ram_max))
//	RAM             | Σ ceil(ram / ram_max) / serveur          | Σ ceil(Σram / ram_max) / cluster           | ceil(Σram / ram_max)
//
// Le niveau GLOBAL n'est pas additif : le chiffre a un sens sur le groupe
// « parc entier » ; un sous-total par projet est une répartition indicative
// et la somme des sous-totaux peut dépasser le total. C'est pour cela que
// ces colonnes se calculent par groupe (RegrouperAvecLicences) et jamais par
// somme de lignes comme les autres agrégats.

// composantRam est le code du composant canonique porté par Capacites pour
// la RAM (docs/import-format.md, depot.ComposantsCanoniques).
const composantRam = "ram"

// DemandeLicences indique si l'une des deux colonnes de licences figure
// parmi les colonnes demandées.
func DemandeLicences(colonnes []Colonne) bool {
	for _, c := range colonnes {
		if c == ColLicencesUnites || c == ColLicencesCout {
			return true
		}
	}
	return false
}

// AxesEffectifs renvoie les axes à utiliser réellement : quand une colonne
// de licences est demandée sans que la techno soit un axe, l'axe techno est
// ajouté en fin — le tableau se découpe automatiquement par techno, parce
// que des unités de technos différentes ne s'additionnent pas (le coût,
// si). Sinon, les axes sont rendus tels quels.
func AxesEffectifs(axes []Dimension, colonnes []Colonne) []Dimension {
	if !DemandeLicences(colonnes) {
		return axes
	}
	for _, a := range axes {
		if a == DimTechno {
			return axes
		}
	}
	out := make([]Dimension, 0, len(axes)+1)
	out = append(out, axes...)
	return append(out, DimTechno)
}

// RegrouperAvecLicences est Regrouper augmenté des colonnes de licences :
// axes effectifs (AxesEffectifs), regroupement, puis calcul des unités et du
// coût par groupe via CalculerLicences avec les contrats résolus (par
// identifiant de techno, voir depot.ResoudreContrats). Les autres colonnes
// restent des sommes. Renvoie aussi les axes effectifs, pour que l'appelant
// affiche les bons libellés d'en-tête quand l'axe techno a été ajouté.
func RegrouperAvecLicences(lignes []Ligne, axes []Dimension, colonnes []Colonne, contrats map[int64]depot.LicenceContrat) ([]Groupe, []Dimension) {
	effectifs := AxesEffectifs(axes, colonnes)
	if contrats == nil {
		contrats = map[int64]depot.LicenceContrat{}
	}
	return regrouper(lignes, effectifs, colonnes, contrats), effectifs
}

// CalculerLicences applique, techno par techno présente dans lignes, le
// contrat de cette techno (absent : la techno compte 0) et renvoie les
// unités cumulées et leur coût (unités × coût unitaire, 0 sans coût).
//
// Les lignes d'un groupe mêlent en général plusieurs technos quand l'axe
// techno n'est pas choisi : les unités s'additionnent alors entre technos —
// c'est précisément ce que RegrouperAvecLicences évite en ajoutant l'axe,
// mais la fonction reste correcte sur n'importe quel ensemble de lignes.
func CalculerLicences(lignes []Ligne, contrats map[int64]depot.LicenceContrat) (unites, cout float64) {
	parTechno := map[int64][]Ligne{}
	for _, l := range lignes {
		parTechno[l.TechnoID] = append(parTechno[l.TechnoID], l)
	}
	for technoID, ls := range parTechno {
		contrat, ok := contrats[technoID]
		if !ok {
			continue
		}
		u := unitesContrat(ls, contrat)
		unites += u
		if contrat.CoutUnitaireHT != nil {
			cout += u * *contrat.CoutUnitaireHT
		}
	}
	return unites, cout
}

// unitesContrat évalue le mécanisme d'un contrat au niveau demandé sur les
// lignes d'une seule techno.
func unitesContrat(lignes []Ligne, contrat depot.LicenceContrat) float64 {
	if contrat.Mecanisme == depot.MecanismeLicenceNoeuds {
		// niveau sans objet : une somme reste une somme
		var total float64
		for _, l := range lignes {
			total += l.NbNoeuds
		}
		return total
	}

	var ramMax float64
	if contrat.RamMaxGo != nil {
		ramMax = *contrat.RamMaxGo
	}
	formule := func(nb, ram float64) float64 {
		unitesRam := plafondQuotient(ram, ramMax)
		if contrat.Mecanisme == depot.MecanismeLicenceMaxNoeudsRam {
			return math.Max(nb, unitesRam)
		}
		return unitesRam // RAM
	}

	switch contrat.Niveau {
	case depot.NiveauLicenceMachine:
		var total float64
		for _, l := range lignes {
			total += formule(l.NbNoeuds, l.Capacites[composantRam])
		}
		return total
	case depot.NiveauLicenceCluster:
		type somme struct{ nb, ram float64 }
		parCluster := map[int64]*somme{}
		for _, l := range lignes {
			s, ok := parCluster[l.ClusterID]
			if !ok {
				s = &somme{}
				parCluster[l.ClusterID] = s
			}
			s.nb += l.NbNoeuds
			s.ram += l.Capacites[composantRam]
		}
		var total float64
		for _, s := range parCluster {
			total += formule(s.nb, s.ram)
		}
		return total
	default: // GLOBAL
		var nb, ram float64
		for _, l := range lignes {
			nb += l.NbNoeuds
			ram += l.Capacites[composantRam]
		}
		return formule(nb, ram)
	}
}

// plafondQuotient renvoie ceil(ram / ramMax), 0 si l'un des deux est nul ou
// négatif (un contrat RAM sans RAM max ne passe pas la validation du dépôt,
// mais une lecture ne doit jamais diviser par zéro). Une petite tolérance
// absorbe le bruit de virgule flottante d'une somme de capacités : 3
// exactement ne doit pas devenir 4 parce que 0,1 + 0,2 vaut 0,30000000000000004.
func plafondQuotient(ram, ramMax float64) float64 {
	if ram <= 0 || ramMax <= 0 {
		return 0
	}
	q := ram / ramMax
	return math.Ceil(q - 1e-9)
}
