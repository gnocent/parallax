package web

import (
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"parallax/internal/depot"
	"parallax/internal/vues"
)

// handlers_capacite.go — la vue capacité : en lignes, les clusters
// regroupés selon les axes usuels (projet, environnement, techno, tier,
// usage, cluster — et l'année de planification) ; en colonnes, un composant
// par colonne avec deux sous-colonnes, besoin et capacité installée ; pour
// le réel ou un scénario.
//
// Le besoin est une grandeur par cluster (une règle s'évalue sur son
// périmètre), pas par serveur : c'est pourquoi cette vue est un écran à part
// et non des colonnes de plus dans le constructeur de vues (par serveur,
// avec des axes plus fins — zone, modèle, statut — sur lesquels un besoin
// n'a pas de sens). Par cluster et par composant, le besoin retenu est le
// MAXIMUM des règles actives qui visent ce composant — comme dans l'ordre
// de dimensionnement (CLAUDE.md), deux règles sur un même composant sont
// des contraintes concurrentes, pas additives. Les besoins des clusters
// d'un groupe s'additionnent ensuite, comme leurs capacités.
//
// L'axe « année » fait de la vue une projection pluriannuelle : une ligne
// par année de la plage [annee, annee_fin] (dix au plus), chaque année
// recalculée avec ses variables et son offre lue au 31 décembre. Sans cet
// axe, seule annee compte. Pas de ligne Total sur plusieurs années : des
// besoins d'années différentes ne s'additionnent pas.
//
//	GET /capacite    ?annee=&annee_fin=&scenario=&axe1=&axe2=&axe3=   (export universel : ?export=csv|xlsx)
func (s *serveur) routesCapacite() {
	s.mux.HandleFunc("GET /capacite", s.lecteur(s.capacitePage))
}

// axeAnnee est l'axe propre à cette vue — l'année de planification, pas
// une dimension du parc — d'où une constante locale plutôt qu'une entrée de
// vues.Dimensions.
const axeAnnee vues.Dimension = "annee"

const maxAnneesCapacite = 10

// axesCapacite : les dimensions d'un cluster, dans l'ordre proposé — le
// sous-ensemble de vues.Dimensions sur lequel un besoin se somme — plus
// l'année.
var axesCapacite = []vues.Dimension{
	axeAnnee, vues.DimProjet, vues.DimEnvironnement, vues.DimTechno, vues.DimTier, vues.DimUsage, vues.DimCluster,
}

func axeCapaciteValide(v string) bool {
	for _, a := range axesCapacite {
		if string(a) == v {
			return true
		}
	}
	return false
}

func libelleAxeCapacite(d vues.Dimension) string {
	if d == axeAnnee {
		return "Année"
	}
	return vues.LibelleDimension(d)
}

// axesCapaciteDepuisRequete lit axe1..axe3 ; sans aucun paramètre d'axe, la
// vue classique techno × tier (la maille des règles de capacité, en
// pratique).
func axesCapaciteDepuisRequete(r *http.Request) []vues.Dimension {
	var axes []vues.Dimension
	aucunParametre := true
	for i := 1; i <= maxAxes; i++ {
		v := r.FormValue(fmt.Sprintf("axe%d", i))
		if v != "" {
			aucunParametre = false
		}
		if v != "" && axeCapaciteValide(v) {
			axes = append(axes, vues.Dimension(v))
		}
	}
	if len(axes) == 0 && aucunParametre {
		axes = []vues.Dimension{vues.DimTechno, vues.DimTier}
	}
	return axes
}

// anneesCapacite : la plage d'années à calculer — [annee, annee_fin] si
// l'axe année est choisi et annee_fin > annee, bornée à
// maxAnneesCapacite ; sinon la seule annee.
func anneesCapacite(r *http.Request, annee int, axes []vues.Dimension) []int {
	avecAxe := false
	for _, a := range axes {
		if a == axeAnnee {
			avecAxe = true
		}
	}
	fin, err := strconv.Atoi(r.FormValue("annee_fin"))
	if !avecAxe || err != nil || fin <= annee {
		return []int{annee}
	}
	if fin-annee+1 > maxAnneesCapacite {
		fin = annee + maxAnneesCapacite - 1
	}
	annees := make([]int, 0, fin-annee+1)
	for a := annee; a <= fin; a++ {
		annees = append(annees, a)
	}
	return annees
}

type valeurCapacite struct{ Besoin, Capa float64 }

func (v valeurCapacite) Deficit() bool { return v.Besoin > v.Capa }

// groupeCapacite est une ligne du tableau : les valeurs des axes, le nombre
// de clusters et de serveurs regroupés, et besoin / capa par composant, dans
// l'ordre des colonnes.
type groupeCapacite struct {
	Cles       []string
	Spans      []int // rowspan par axe (fusionsVerticales) ; 0 = cellule non rendue
	NbClusters int
	NbServeurs int
	Valeurs    []valeurCapacite
}

type resultatCapacite struct {
	Axes       []vues.Dimension
	Annees     []int
	Composants []string // colonnes : codes de composant visés par une règle active
	Groupes    []groupeCapacite
	// Total n'a de sens que sur une seule année (AvecTotal) : des besoins
	// d'années différentes ne s'additionnent pas.
	Total     groupeCapacite
	AvecTotal bool
	// Notes : un cluster dont l'évaluation a échoué (variable sans valeur pour
	// l'année…) — sa capacité compte, son besoin non ; jamais silencieux.
	Notes []string
	// Entetes (tri.go), par identifiant : "clusters", "serveurs", puis
	// "<composant>:besoin" et "<composant>:capa" — avec l'URL du tri, cet
	// écran étant en navigation GET classique.
	Entetes map[string]enteteTri
}

func (res resultatCapacite) LibellesAxes() []string { return libelles(res.Axes, libelleAxeCapacite) }

// trier ordonne les groupes par la colonne demandée (dans leur groupe
// parent, voir ordreTri), recalcule la fusion verticale et pose les
// en-têtes cliquables.
func (res *resultatCapacite) trier(r *http.Request) {
	tri, desc := triDepuisRequete(r)
	res.Entetes = map[string]enteteTri{}
	poser := func(code string) {
		e := nouvelEnteteTri(code, code, tri, desc)
		e.URL = lienTri(r, e)
		res.Entetes[code] = e
	}
	poser("clusters")
	poser("serveurs")
	for _, c := range res.Composants {
		poser(c + ":besoin")
		poser(c + ":capa")
	}
	valeur := func(i int) float64 {
		g := res.Groupes[i]
		switch tri {
		case "clusters":
			return float64(g.NbClusters)
		case "serveurs":
			return float64(g.NbServeurs)
		}
		for j, c := range res.Composants {
			switch tri {
			case c + ":besoin":
				return g.Valeurs[j].Besoin
			case c + ":capa":
				return g.Valeurs[j].Capa
			}
		}
		return 0
	}
	if _, ok := res.Entetes[tri]; !ok {
		return
	}
	cles := make([][]string, len(res.Groupes))
	for i, g := range res.Groupes {
		cles[i] = g.Cles
	}
	triees := make([]groupeCapacite, len(res.Groupes))
	for i, j := range ordreTri(cles, valeur, desc) {
		triees[i] = res.Groupes[j]
	}
	res.Groupes = triees
	for i, g := range res.Groupes {
		cles[i] = g.Cles
	}
	for i, spans := range fusionsVerticales(cles) {
		res.Groupes[i].Spans = spans
	}
}

// calculerCapacite évalue chaque cluster actif (besoinOffreCluster, le même
// calcul que la page besoin/offre), pour chaque année demandée, puis
// regroupe.
func (s *serveur) calculerCapacite(annees []int, scenarioID *int64, axes []vues.Dimension) (resultatCapacite, error) {
	clusters, err := s.depot.ListerClusters(depot.FiltreCluster{})
	if err != nil {
		return resultatCapacite{}, err
	}
	lc, err := s.chargerLibellesCluster()
	if err != nil {
		return resultatCapacite{}, err
	}
	regles, err := s.depot.ReglesCapaciteActives(0)
	if err != nil {
		return resultatCapacite{}, err
	}

	// colonnes : les composants visés par au moins une règle active, dans
	// l'ordre canonique, puis les codes libres par ordre alphabétique.
	vises := map[string]bool{}
	for _, rg := range regles {
		if rg.ComposantOffre != "" {
			vises[rg.ComposantOffre] = true
		}
	}
	var composants []string
	for _, c := range depot.ComposantsCanoniques {
		if vises[c.Code] {
			composants = append(composants, c.Code)
			delete(vises, c.Code)
		}
	}
	var libres []string
	for code := range vises {
		libres = append(libres, code)
	}
	sort.Strings(libres)
	composants = append(composants, libres...)
	index := make(map[string]int, len(composants))
	for i, c := range composants {
		index[c] = i
	}

	res := resultatCapacite{
		Axes: axes, Annees: annees, Composants: composants, AvecTotal: len(annees) == 1,
		Total: groupeCapacite{Valeurs: make([]valeurCapacite, len(composants))},
	}
	groupes := map[string]*groupeCapacite{}
	var ordre []string
	for _, annee := range annees {
		ctx, err := s.chargerContexteBesoin(annee, scenarioID)
		if err != nil {
			return resultatCapacite{}, err
		}
		aDate := dateReferenceOffre(annee)
		for _, c := range clusters {
			lignes, offre, err := s.besoinOffreCluster(ctx, c, scenarioID, aDate)
			if err != nil {
				var ev erreurEvaluation
				if !errors.As(err, &ev) {
					return resultatCapacite{}, err
				}
				note := c.Nom + " : " + ev.Error()
				if len(annees) > 1 {
					note = strconv.Itoa(annee) + ", " + note
				}
				res.Notes = append(res.Notes, note)
			}
			nbServeurs, err := s.nbServeursCluster(c.ID, scenarioID, aDate)
			if err != nil {
				return resultatCapacite{}, err
			}

			cles := clesCapacite(c, annee, axes, lc)
			cle := strings.Join(cles, "\x1f")
			g, ok := groupes[cle]
			if !ok {
				g = &groupeCapacite{Cles: cles, Valeurs: make([]valeurCapacite, len(composants))}
				groupes[cle] = g
				ordre = append(ordre, cle)
			}
			g.NbClusters++
			g.NbServeurs += nbServeurs
			res.Total.NbClusters++
			res.Total.NbServeurs += nbServeurs

			besoinParComposant := map[string]float64{}
			for _, l := range lignes {
				if l.Besoin > besoinParComposant[l.ComposantOffre] {
					besoinParComposant[l.ComposantOffre] = l.Besoin
				}
			}
			for code, i := range index {
				b, capa := besoinParComposant[code], offre[code]
				g.Valeurs[i].Besoin += b
				g.Valeurs[i].Capa += capa
				res.Total.Valeurs[i].Besoin += b
				res.Total.Valeurs[i].Capa += capa
			}
		}
	}
	sort.Strings(ordre)
	cles := make([][]string, 0, len(ordre))
	for _, cle := range ordre {
		res.Groupes = append(res.Groupes, *groupes[cle])
		cles = append(cles, groupes[cle].Cles)
	}
	for i, spans := range fusionsVerticales(cles) {
		res.Groupes[i].Spans = spans
	}
	return res, nil
}

func clesCapacite(c depot.Cluster, annee int, axes []vues.Dimension, lc libellesCluster) []string {
	cles := make([]string, len(axes))
	for i, a := range axes {
		switch a {
		case axeAnnee:
			cles[i] = strconv.Itoa(annee)
		case vues.DimProjet:
			cles[i] = lc.Projets[c.ProjetID]
		case vues.DimEnvironnement:
			cles[i] = lc.Environnements[c.EnvironnementID]
		case vues.DimTechno:
			cles[i] = lc.Technos[c.TechnoID]
		case vues.DimTier:
			if c.TierID != nil {
				cles[i] = lc.Tiers[*c.TierID]
			}
		case vues.DimUsage:
			if c.UsageFonctionnelID != nil {
				cles[i] = lc.Usages[*c.UsageFonctionnelID]
			}
		case vues.DimCluster:
			cles[i] = c.Nom
		}
	}
	return cles
}

func (s *serveur) nbServeursCluster(clusterID int64, scenarioID *int64, aDate string) (int, error) {
	affectations, err := s.depot.AffectationsResolues(clusterID, scenarioID, aDate)
	if err != nil {
		return 0, err
	}
	return len(affectations), nil
}

func tableauCapacite(nomScenario string, res resultatCapacite) tableau {
	titre := fmt.Sprintf("Capacité — %s — %d", nomScenario, res.Annees[0])
	if len(res.Annees) > 1 {
		titre = fmt.Sprintf("Capacité — %s — %d-%d", nomScenario, res.Annees[0], res.Annees[len(res.Annees)-1])
	}
	t := tableau{Titre: titre}
	t.Colonnes = append(t.Colonnes, res.LibellesAxes()...)
	t.Colonnes = append(t.Colonnes, "Clusters", "Serveurs")
	for _, c := range res.Composants {
		t.Colonnes = append(t.Colonnes, c+" besoin", c+" capa")
	}
	ligne := func(g groupeCapacite, total bool) []any {
		var l []any
		for i, cle := range g.Cles {
			if total {
				cle = ""
				if i == 0 {
					cle = "Total"
				}
			}
			l = append(l, cle)
		}
		l = append(l, g.NbClusters, g.NbServeurs)
		for _, v := range g.Valeurs {
			l = append(l, v.Besoin, v.Capa)
		}
		return l
	}
	for _, g := range res.Groupes {
		t.Lignes = append(t.Lignes, ligne(g, false))
	}
	if len(res.Groupes) > 0 && res.AvecTotal {
		total := res.Total
		total.Cles = make([]string, len(res.Axes))
		t.Lignes = append(t.Lignes, ligne(total, true))
	}
	return t
}

func (s *serveur) capacitePage(w http.ResponseWriter, r *http.Request) {
	annee := anneeDepuisRequete(r)
	scenarioID := scenarioDepuisRequete(r)
	axes := axesCapaciteDepuisRequete(r)
	annees := anneesCapacite(r, annee, axes)
	res, err := s.calculerCapacite(annees, scenarioID, axes)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	res.trier(r)
	if s.exporter(w, r, func() (tableau, error) {
		return tableauCapacite(s.nomScenario(scenarioID), res), nil
	}) {
		return
	}
	selecteur, err := s.chargerSelecteurScenarios(r)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	dimensions := make([]optionDimension, len(axesCapacite))
	for i, d := range axesCapacite {
		dimensions[i] = optionDimension{Code: string(d), Libelle: libelleAxeCapacite(d)}
	}
	codes := make([]string, len(axes))
	for i, a := range axes {
		codes[i] = string(a)
	}
	anneeFin := annees[len(annees)-1]
	if v, err := strconv.Atoi(r.FormValue("annee_fin")); err == nil && v > anneeFin {
		anneeFin = v // conservé tel que saisi, même sans l'axe année
	}
	s.rendrePage(w, r, s.titre(r, "titre.capacite"), "capacite_page", map[string]any{
		"Annee":         annee,
		"AnneeFin":      anneeFin,
		"DateOffre":     dateReferenceOffre(annee),
		"Scenarios":     selecteur.Scenarios,
		"ScenarioID":    selecteur.ScenarioActifID,
		"SelecteurAxes": nouveauSelecteurAxes(dimensions, codes),
		"Resultat":      res,
		"Export":        liensExportPour(r, ""),
	})
}
