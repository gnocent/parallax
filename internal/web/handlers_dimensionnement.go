package web

import (
	"errors"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"parallax/internal/capacity"
	"parallax/internal/depot"
)

// dateReferenceOffre est la date à laquelle l'offre installée (et le delta
// d'un scénario) se lisent pour une année de planification : son 31 décembre.
// Lire l'offre « à la date du jour » rendait invisible toute hypothèse datée
// dans l'année visée — un serveur ajouté « en 2027 » n'apparaissait jamais
// dans le besoin/offre 2027, l'usage même de l'outil. Pour une année passée,
// c'est l'état du parc à la fin de cette année (v1.3 : consultation à une
// date passée), révisions comprises.
func dateReferenceOffre(annee int) string { return fmt.Sprintf("%04d-12-31", annee) }

// erreurEvaluation signale un problème de formule ou de variable (règle mal
// écrite, variable sans valeur pour l'année demandée) : une erreur à montrer
// à l'utilisateur sur la page, pas une panne du serveur.
type erreurEvaluation struct{ cause error }

func (e erreurEvaluation) Error() string { return e.cause.Error() }

// routesDimensionnement ajoute l'écran besoin / offre / écart par cluster
// (v1.5) : pour chaque règle active applicable, le besoin calculé par le
// moteur de formules face à l'offre déjà installée, avec identification de
// la règle limitante.
//
// Depuis la v2.1, paramétrable par scénario (paramètre "scenario", voir
// scenario_contexte.go) : c'est l'écran unique par cluster visé par le point
// de vigilance ergonomique du backlog — état courant, delta appliqué par le
// scénario et capacité résultante s'y lisent sans changer de page. La portée
// temporelle reste celle de v1 : l'état courant, pas une date passée.
func (s *serveur) routesDimensionnement() {
	s.mux.HandleFunc("GET /clusters/{id}/besoin-offre", s.lecteur(s.clusterBesoinOffre))
}

// ligneBesoinOffre est une règle confrontée à l'offre installée.
type ligneBesoinOffre struct {
	RegleID        int64
	Nom            string
	Metrique       string
	ComposantOffre string
	Besoin         float64
	Offre          float64
	Ecart          float64 // offre - besoin ; négatif = déficit
	Limitante      bool
}

func (s *serveur) clusterBesoinOffre(w http.ResponseWriter, r *http.Request) {
	id, err := idChemin(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	cluster, err := s.depot.LireCluster(id)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	annee := anneeDepuisRequete(r)
	scenarioID := scenarioDepuisRequete(r)
	aDate := dateReferenceOffre(annee)

	var messageEvaluation string
	lignes, err := s.calculerBesoinOffre(cluster, annee, scenarioID, aDate)
	if err != nil {
		var ev erreurEvaluation
		if !errors.As(err, &ev) {
			s.erreurServeur(w, r, err)
			return
		}
		messageEvaluation = "Calcul impossible : " + ev.Error()
	}

	// le delta ne se calcule (et ne s'affiche) que sous un scénario : sous le
	// réel, l'écran garde exactement l'apparence de la v1.
	var delta []ligneDelta
	if scenarioID != nil {
		delta, err = s.calculerDeltaAffectations(id, *scenarioID, aDate)
		if err != nil {
			s.erreurServeur(w, r, err)
			return
		}
	}

	// export universel (tableau.go), quatre tableaux nommés : besoin/offre et
	// delta (cette page), contraintes et contraintes effectives (la section
	// chargée en htmx depuis handlers_contrainte.go, dont les boutons visent
	// cette page). Tous sous l'année et le scénario de la requête.
	if s.exporterNomme(w, r, "besoin_offre", func() (tableau, error) {
		if messageEvaluation != "" {
			return tableau{}, fmt.Errorf("export besoin/offre du cluster %d : %s", id, messageEvaluation)
		}
		return tableauBesoinOffre(cluster, annee, lignes), nil
	}) {
		return
	}
	if s.exporterNomme(w, r, "delta", func() (tableau, error) {
		return tableauDelta(cluster, delta), nil
	}) {
		return
	}
	if s.exporterNomme(w, r, "contraintes", func() (tableau, error) {
		section, err := s.chargerContraintesSection(id, annee, scenarioID, "")
		if err != nil {
			return tableau{}, err
		}
		return tableauContraintes(section), nil
	}) {
		return
	}
	if s.exporterNomme(w, r, "contraintes_effectives", func() (tableau, error) {
		section, err := s.chargerContraintesSection(id, annee, scenarioID, "")
		if err != nil {
			return tableau{}, err
		}
		return tableauContraintesEffectives(section), nil
	}) {
		return
	}

	selecteur, err := s.chargerSelecteurScenarios(r)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}

	donnees := map[string]any{
		"Cluster":           cluster,
		"Annee":             annee,
		"DateOffre":         aDate,
		"Lignes":            lignes,
		"Erreur":            messageEvaluation,
		"Scenarios":         selecteur.Scenarios,
		"ScenarioID":        selecteur.ScenarioActifID,
		"ExportBesoinOffre": liensExportPour(r, "besoin_offre"),
		"ExportDelta":       liensExportPour(r, "delta"),
	}
	if scenarioID != nil {
		donnees["Delta"] = delta
	}

	s.rendrePage(w, r, s.titre(r, "titre.besoin_offre")+" — "+cluster.Nom, "cluster_besoin_offre_page", donnees)
}

// tableauBesoinOffre est la forme exportable du tableau besoin / offre /
// écart : les mêmes colonnes, valeurs brutes, et le drapeau « limitante ».
func tableauBesoinOffre(cluster depot.Cluster, annee int, lignes []ligneBesoinOffre) tableau {
	t := tableau{Titre: fmt.Sprintf("Besoin / offre — %s — %d", cluster.Nom, annee), Colonnes: []string{
		"Règle", "Métrique", "Composant", "Besoin", "Offre", "Écart", "Limitante",
	}}
	for _, l := range lignes {
		t.Lignes = append(t.Lignes, []any{l.Nom, l.Metrique, l.ComposantOffre, l.Besoin, l.Offre, l.Ecart, l.Limitante})
	}
	return t
}

// tableauDelta est la forme exportable du delta d'un scénario sur le cluster,
// avec le détail en toutes lettres tel que l'écran l'affiche.
func tableauDelta(cluster depot.Cluster, delta []ligneDelta) tableau {
	t := tableau{Titre: "Delta — " + cluster.Nom, Colonnes: []string{"Mouvement", "Serveur", "Détail"}}
	for _, l := range delta {
		detail := "nouveau sur ce cluster sous le scénario"
		if l.Mouvement == "DEPART" {
			detail = "retiré, sans réaffectation"
			if l.AutreCluster != "" {
				detail = "déplacé vers " + l.AutreCluster
			}
		}
		t.Lignes = append(t.Lignes, []any{l.Mouvement, l.PhysicalName, detail})
	}
	return t
}

// ligneDelta décrit un serveur dont la présence sur ce cluster change sous le
// scénario par rapport au réel — voir modele-donnees.md §6 pour la règle de
// résolution qui en fait la base.
type ligneDelta struct {
	ServeurID    int64
	PhysicalName string
	Mouvement    string // ARRIVEE | DEPART
	AutreCluster string // pour un départ : le cluster qui le reçoit sous le scénario, vide s'il est simplement retiré
}

// calculerDeltaAffectations confronte l'affectation réelle et l'affectation
// résolue sous le scénario, aujourd'hui, pour identifier les arrivées et les
// départs de ce cluster précis — la traduction visible du mécanisme de
// surcharge d'AffectationsResolues.
func (s *serveur) calculerDeltaAffectations(clusterID int64, scenarioID int64, aDate string) ([]ligneDelta, error) {
	reel, err := s.depot.AffectationsResolues(clusterID, nil, aDate)
	if err != nil {
		return nil, err
	}
	sousScenario, err := s.depot.AffectationsResolues(clusterID, &scenarioID, aDate)
	if err != nil {
		return nil, err
	}
	dansReel := map[int64]bool{}
	for _, a := range reel {
		dansReel[a.ServeurID] = true
	}
	dansScenario := map[int64]bool{}
	for _, a := range sousScenario {
		dansScenario[a.ServeurID] = true
	}

	var out []ligneDelta
	for _, a := range sousScenario {
		if !dansReel[a.ServeurID] {
			out = append(out, ligneDelta{ServeurID: a.ServeurID, Mouvement: "ARRIVEE"})
		}
	}
	for _, a := range reel {
		if dansScenario[a.ServeurID] {
			continue // toujours présent, réel ou déplacé ailleurs mais pas ici -> déjà traité ci-dessus s'il revient
		}
		ligne := ligneDelta{ServeurID: a.ServeurID, Mouvement: "DEPART"}
		// où est-il passé sous le scénario, s'il y est toujours (déplacé plutôt
		// que retiré) ? AffectationEffectiveServeur répond sans requêter tout
		// un autre cluster candidat.
		effective, err := s.depot.AffectationEffectiveServeur(a.ServeurID, &scenarioID, aDate)
		if err != nil {
			return nil, err
		}
		if effective != nil {
			c, err := s.depot.LireCluster(effective.ClusterID)
			if err == nil {
				ligne.AutreCluster = c.Nom
			}
		}
		out = append(out, ligne)
	}

	if len(out) == 0 {
		return out, nil
	}
	ids := make([]int64, len(out))
	for i, l := range out {
		ids[i] = l.ServeurID
	}
	noms, err := s.depot.NomsPhysiquesServeurs(ids)
	if err != nil {
		return nil, err
	}
	for i := range out {
		out[i].PhysicalName = noms[out[i].ServeurID]
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Mouvement != out[j].Mouvement {
			return out[i].Mouvement < out[j].Mouvement // ARRIVEE avant DEPART
		}
		return out[i].ServeurID < out[j].ServeurID
	})
	return out, nil
}

// calculerBesoinOffre évalue le besoin de chaque règle active applicable au
// cluster (internal/capacity.CalculerBesoins, la même brique que
// Dimensionner) et le compare à l'offre actuellement installée : la somme,
// par code de composant, des capacités des révisions des serveurs affectés
// au cluster à la date du jour.
//
// scenarioID nil calcule sur le réel, exactement comme en v1. Une valeur
// recalcule sur l'offre résolue sous le scénario (depot.AffectationsResolues)
// et sur les variables en tenant compte de ses surcharges — CalculerBesoins
// préfère déjà la valeur du scénario à celle du réel dès que
// EntreeDimensionnement.ScenarioID et Valeurs la portent (voir
// capacity.ResoudreVariable) : il ne manquait que ce fil à tirer depuis
// l'écran.
//
// Convention de nommage des variables dérivées des composants, alignée sur
// les exemples du cadrage du projet : un composant de code « ram » expose
// « ram_total » au niveau périmètre et « ram_machine » par serveur. C'est ce
// qui permet à une règle PAR_SERVEUR de s'écrire ceil(ram_machine / ram_max).
func (s *serveur) calculerBesoinOffre(cluster depot.Cluster, annee int, scenarioID *int64, aDate string) ([]ligneBesoinOffre, error) {
	regles, err := s.depot.ReglesCapaciteActives(0)
	if err != nil {
		return nil, err
	}

	valeurs, err := s.chargerValeursAnnee(annee, scenarioID)
	if err != nil {
		return nil, err
	}
	defauts, err := s.chargerDefautsVariables()
	if err != nil {
		return nil, err
	}

	serveurs, offreParCode, err := s.chargerOffreCluster(cluster.ID, scenarioID, aDate)
	if err != nil {
		return nil, err
	}
	// une variable par composant de code "x" existe potentiellement au
	// périmètre ("x_total") et par serveur ("x_machine") : garantir une
	// valeur par défaut pour les deux évite un échec de résolution sur une
	// règle qui référence un composant absent de ce cluster précis.
	agregats := map[string]float64{"nb_serveurs": float64(len(serveurs))}
	for code, total := range offreParCode {
		agregats[code+"_total"] = total
		if _, ok := defauts[code+"_machine"]; !ok {
			defauts[code+"_machine"] = 0
		}
		if _, ok := defauts[code+"_total"]; !ok {
			defauts[code+"_total"] = 0
		}
	}

	capaCluster := capacity.Cluster{
		ID: cluster.ID, ProjetID: &cluster.ProjetID, EnvironnementID: &cluster.EnvironnementID,
		TechnoID: &cluster.TechnoID, TierID: cluster.TierID, UsageID: cluster.UsageFonctionnelID,
	}

	besoins, err := capacity.CalculerBesoins(capacity.EntreeDimensionnement{
		Cluster: capaCluster, Regles: regles, Valeurs: valeurs, Defauts: defauts,
		Annee: annee, ScenarioID: scenarioID, Agregats: agregats, Serveurs: serveurs,
	})
	if err != nil {
		return nil, erreurEvaluation{cause: err}
	}

	lignes := make([]ligneBesoinOffre, len(besoins))
	limitanteIdx := -1
	meilleurRatio := 0.0
	for i, b := range besoins {
		offre := offreParCode[b.ComposantOffre]
		l := ligneBesoinOffre{
			RegleID: b.RegleID, Nom: b.Nom, Metrique: b.Metrique,
			ComposantOffre: b.ComposantOffre, Besoin: b.Besoin, Offre: offre, Ecart: offre - b.Besoin,
		}
		lignes[i] = l

		if b.Besoin <= 0 {
			continue
		}
		ratio := b.Besoin / offre // offre nulle → +Inf, une règle sans offre est la plus limitante possible
		if offre <= 0 {
			ratio = math.Inf(1)
		}
		if limitanteIdx == -1 || ratio > meilleurRatio {
			limitanteIdx, meilleurRatio = i, ratio
		}
	}
	if limitanteIdx >= 0 {
		lignes[limitanteIdx].Limitante = true
	}

	sort.Slice(lignes, func(i, j int) bool { return lignes[i].RegleID < lignes[j].RegleID })
	return lignes, nil
}

// anneeDepuisRequete lit le paramètre "annee" en GET (query) comme en POST
// (corps) — r.FormValue couvre les deux, comme scenarioDepuisRequete. Défaut :
// l'année courante.
func anneeDepuisRequete(r *http.Request) int {
	if v, err := strconv.Atoi(r.FormValue("annee")); err == nil && v > 0 {
		return v
	}
	return time.Now().Year()
}

// chargerValeursAnnee charge toutes les valeurs de variables pour une année,
// converties au format internal/capacity — nécessaire parce que
// CalculerBesoins peut référencer des variables de codes différents selon
// les règles, pas une seule à la fois comme depot.ResoudreValeur.
//
// scenarioID nil ne charge que le réel. Une valeur charge aussi les
// surcharges de ce scénario, ScenarioID renseigné sur chaque ligne : c'est
// capacity.ResoudreVariable (appelé par CalculerBesoins), pas cette
// fonction, qui arbitre scénario contre réel à la résolution.
func (s *serveur) chargerValeursAnnee(annee int, scenarioID *int64) ([]capacity.ValeurVariable, error) {
	requete := `SELECT v.code, vv.scenario_id, vv.projet_id, vv.environnement_id, vv.techno_id,
	                   vv.tier_id, vv.cluster_id, vv.valeur
	            FROM variable_valeur vv JOIN variable v ON v.id = vv.variable_id
	            WHERE vv.annee = ? AND (vv.scenario_id IS NULL`
	args := []any{annee}
	if scenarioID != nil {
		requete += ` OR vv.scenario_id = ?`
		args = append(args, *scenarioID)
	}
	requete += `)`

	lignes, err := s.depot.Base().Query(requete, args...)
	if err != nil {
		return nil, err
	}
	defer lignes.Close()

	var out []capacity.ValeurVariable
	for lignes.Next() {
		var v capacity.ValeurVariable
		v.Annee = annee
		if err := lignes.Scan(&v.Code, &v.ScenarioID, &v.ProjetID, &v.EnvironnementID,
			&v.TechnoID, &v.TierID, &v.ClusterID, &v.Valeur); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, lignes.Err()
}

func (s *serveur) chargerDefautsVariables() (map[string]float64, error) {
	lignes, err := s.depot.Base().Query(`SELECT code, defaut FROM variable WHERE defaut IS NOT NULL`)
	if err != nil {
		return nil, err
	}
	defer lignes.Close()

	out := map[string]float64{}
	for lignes.Next() {
		var code string
		var defaut float64
		if err := lignes.Scan(&code, &defaut); err != nil {
			return nil, err
		}
		out[code] = defaut
	}
	return out, lignes.Err()
}

// chargerOffreCluster renvoie, pour les serveurs occupant effectivement le
// cluster à aDate (dateReferenceOffre) : une entrée Serveurs par serveur
// (variables "<code>_machine"), et la somme par code de composant sur
// l'ensemble du périmètre (pour les variables "<code>_total" et pour comparer
// chaque règle à son offre). La révision retenue pour chaque serveur est celle
// rattachée à cette date, pas forcément la courante.
//
// scenarioID nil ne considère que le réel. Une valeur utilise
// depot.AffectationsResolues : l'offre installée sous le scénario reflète
// alors ses déplacements et retraits, pas seulement ses ajouts.
func (s *serveur) chargerOffreCluster(clusterID int64, scenarioID *int64, aDate string) ([]map[string]float64, map[string]float64, error) {
	affectations, err := s.depot.AffectationsResolues(clusterID, scenarioID, aDate)
	if err != nil {
		return nil, nil, err
	}
	if len(affectations) == 0 {
		return nil, map[string]float64{}, nil
	}

	parServeur := map[int64]map[string]float64{}
	ordre := make([]int64, len(affectations))
	marques := make([]string, len(affectations))
	args := make([]any, len(affectations))
	for i, a := range affectations {
		parServeur[a.ServeurID] = map[string]float64{}
		ordre[i] = a.ServeurID
		marques[i] = "?"
		args[i] = a.ServeurID
	}

	args = append([]any{aDate, aDate}, args...)
	compLignes, err := s.depot.Base().Query(
		`SELECT sr.serveur_id, comp.code, comp.quantite * comp.capacite_unitaire
		 FROM serveur_revision sr
		 JOIN composant comp ON comp.revision_id = sr.revision_id
		 WHERE sr.date_debut <= ? AND (sr.date_fin IS NULL OR sr.date_fin >= ?)
		   AND sr.serveur_id IN (`+strings.Join(marques, ",")+`)`,
		args...)
	if err != nil {
		return nil, nil, err
	}
	defer compLignes.Close()

	total := map[string]float64{}
	for compLignes.Next() {
		var id int64
		var code string
		var valeur float64
		if err := compLignes.Scan(&id, &code, &valeur); err != nil {
			return nil, nil, err
		}
		parServeur[id][code+"_machine"] += valeur
		total[code] += valeur
	}
	if err := compLignes.Err(); err != nil {
		return nil, nil, err
	}

	serveurs := make([]map[string]float64, len(ordre))
	for i, id := range ordre {
		serveurs[i] = parServeur[id]
	}
	return serveurs, total, nil
}
