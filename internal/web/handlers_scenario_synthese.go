package web

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"parallax/internal/depot"
	"parallax/internal/vues"
)

// handlers_scenario_synthese.go — le point d'entrée d'un scénario, et le
// dimensionnement en lot.
//
// Avant ce fichier, un scénario n'avait pas de page : on le créait dans la
// liste, puis tout se passait cluster par cluster (besoin/offre sous le
// scénario, dimensionnement inverse, matérialisation), et la seule vue
// transversale était /comparaison, à reconfigurer à chaque fois. La synthèse
// donne la vision avant / après tous clusters confondus, avec le lien
// « Dimensionner » là où ça coince ; le lot enchaîne le dimensionnement
// inverse (v2.3) sur tous les clusters d'un coup, avec un modèle candidat
// par tier ou par cluster et un choix global des générations conservées,
// un aperçu ligne par ligne, puis une matérialisation en une transaction
// (depot.MaterialiserLot).
//
//	GET  /scenarios/{id}/synthese          tous les clusters, avant / après
//	GET  /scenarios/{id}/lot               modèles conservés, modèle par tier / cluster
//	GET  /scenarios/{id}/lot/apercu        fragment : ce que le lot poserait
//	POST /scenarios/{id}/lot/materialiser  pose le lot, redirige vers la synthèse
//
// GET /scenarios/{id} est déjà pris par le fragment de ligne de la liste
// (handlers_scenario.go), d'où le suffixe /synthese.
func (s *serveur) routesScenarioSynthese() {
	s.mux.HandleFunc("GET /scenarios/{id}/synthese", s.lecteur(s.scenarioSynthesePage))
	s.mux.HandleFunc("GET /scenarios/{id}/lot", s.lecteur(s.scenarioLotPage))
	s.mux.HandleFunc("GET /scenarios/{id}/lot/apercu", s.lecteur(s.scenarioLotApercu))
	s.mux.HandleFunc("POST /scenarios/{id}/lot/materialiser", s.editeur(s.scenarioLotMaterialiser))
}

// lienComparaisonScenario construit l'URL de /comparaison préréglée « réel
// contre ce scénario », par cluster, avec nombre de serveurs, coûts et
// licences — la comparaison qu'on refaisait à la main à chaque fois. La
// date est celle de lecture de l'offre pour l'année (dateReferenceOffre).
func lienComparaisonScenario(scenarioID int64, annee int) string {
	q := url.Values{}
	q.Set("scenario_a", "0")
	q.Set("scenario_b", strconv.FormatInt(scenarioID, 10))
	q.Set("date", dateReferenceOffre(annee))
	q.Set("axe1", string(vues.DimCluster))
	for _, c := range []vues.Colonne{vues.ColNbServeurs, vues.ColCoutAcquisition, vues.ColCoutAnnuel, vues.ColLicencesUnites, vues.ColLicencesCout} {
		q.Add("colonne", string(c))
	}
	return "/comparaison?" + q.Encode()
}

// clustersDuScenario : les clusters actifs du projet du scénario, ou tous
// les clusters actifs s'il n'est rattaché à aucun projet (le rattachement
// est un classement, pas une restriction — depot.Scenario.ProjetID), triés
// par nom.
func (s *serveur) clustersDuScenario(sc depot.Scenario) ([]depot.Cluster, error) {
	clusters, err := s.depot.ListerClusters(depot.FiltreCluster{ProjetID: sc.ProjetID})
	if err != nil {
		return nil, err
	}
	sort.SliceStable(clusters, func(i, j int) bool { return clusters[i].Nom < clusters[j].Nom })
	return clusters, nil
}

func (s *serveur) lireScenarioChemin(w http.ResponseWriter, r *http.Request) (depot.Scenario, bool) {
	id, err := idChemin(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return depot.Scenario{}, false
	}
	sc, err := s.depot.LireScenario(id)
	if err != nil {
		if errors.Is(err, depot.ErrIntrouvable) {
			http.NotFound(w, r)
		} else {
			s.erreurServeur(w, r, err)
		}
		return depot.Scenario{}, false
	}
	return sc, true
}

// ------------------------------------------------------------- synthèse

type chiffresBesoin struct{ Besoin, Offre, Ecart float64 }

// ligneSynthese est un cluster du scénario : sa règle limitante sous le
// scénario, les mêmes chiffres avant (réel) et après (scénario), le
// mouvement de serveurs et le coût des serveurs ajoutés.
type ligneSynthese struct {
	Cluster       depot.Cluster
	TechnoLibelle string
	TierLibelle   string
	Regle         string
	Composant     string
	Avant, Apres  chiffresBesoin
	NbAvant       int
	NbApres       int
	Arrivees      int
	Departs       int
	// coût des serveurs qui arrivent sur le cluster sous le scénario
	// (hypothétiques ou déplacés), lu sur leur modèle
	CoutAcquisition float64
	CoutAnnuel      float64
	Erreur          string
	SansRegle       bool
}

func (l ligneSynthese) Deficit() bool { return !l.SansRegle && l.Erreur == "" && l.Apres.Ecart < 0 }

type totauxSynthese struct {
	NbAvant, NbApres, Arrivees, Departs int
	CoutAcquisition, CoutAnnuel         float64
	Deficits                            int // clusters encore en écart négatif sous le scénario
}

// calculerSynthese évalue chaque cluster deux fois (réel, scénario) avec le
// même contexte de règles et de variables — chargé une fois — et lit le
// mouvement de serveurs par différence des affectations résolues, comme
// calculerDeltaAffectations sur la page d'un cluster.
func (s *serveur) calculerSynthese(sc depot.Scenario, annee int) ([]ligneSynthese, totauxSynthese, error) {
	clusters, err := s.clustersDuScenario(sc)
	if err != nil {
		return nil, totauxSynthese{}, err
	}
	lc, err := s.chargerLibellesCluster()
	if err != nil {
		return nil, totauxSynthese{}, err
	}
	ctx, err := s.chargerContexteBesoin(annee, &sc.ID)
	if err != nil {
		return nil, totauxSynthese{}, err
	}
	aDate := dateReferenceOffre(annee)

	var lignes []ligneSynthese
	var tot totauxSynthese
	for _, c := range clusters {
		l := ligneSynthese{Cluster: c, TechnoLibelle: lc.Technos[c.TechnoID]}
		if c.TierID != nil {
			l.TierLibelle = lc.Tiers[*c.TierID]
		}

		reel, err := s.depot.AffectationsResolues(c.ID, nil, aDate)
		if err != nil {
			return nil, totauxSynthese{}, err
		}
		sous, err := s.depot.AffectationsResolues(c.ID, &sc.ID, aDate)
		if err != nil {
			return nil, totauxSynthese{}, err
		}
		dansReel := map[int64]bool{}
		for _, a := range reel {
			dansReel[a.ServeurID] = true
		}
		dansScenario := map[int64]bool{}
		var arrivees []int64
		for _, a := range sous {
			dansScenario[a.ServeurID] = true
			if !dansReel[a.ServeurID] {
				arrivees = append(arrivees, a.ServeurID)
			}
		}
		for _, a := range reel {
			if !dansScenario[a.ServeurID] {
				l.Departs++
			}
		}
		l.NbAvant, l.NbApres, l.Arrivees = len(reel), len(sous), len(arrivees)
		l.CoutAcquisition, l.CoutAnnuel, err = s.coutsServeurs(arrivees, aDate)
		if err != nil {
			return nil, totauxSynthese{}, err
		}

		apres, errApres := s.calculerBesoinOffreAvec(ctx, c, &sc.ID, aDate)
		avant, errAvant := s.calculerBesoinOffreAvec(ctx, c, nil, aDate)
		if err := premiereErreurEvaluation(errApres, errAvant); err != nil {
			var ev erreurEvaluation
			if !errors.As(err, &ev) {
				return nil, totauxSynthese{}, err
			}
			l.Erreur = ev.Error()
		} else if len(apres) == 0 {
			l.SansRegle = true
		} else {
			for _, b := range apres {
				if b.Limitante {
					l.Regle, l.Composant = b.Nom, b.ComposantOffre
					l.Apres = chiffresBesoin{b.Besoin, b.Offre, b.Ecart}
					for _, a := range avant {
						if a.RegleID == b.RegleID {
							l.Avant = chiffresBesoin{a.Besoin, a.Offre, a.Ecart}
						}
					}
				}
			}
		}

		tot.NbAvant += l.NbAvant
		tot.NbApres += l.NbApres
		tot.Arrivees += l.Arrivees
		tot.Departs += l.Departs
		tot.CoutAcquisition += l.CoutAcquisition
		tot.CoutAnnuel += l.CoutAnnuel
		if l.Deficit() {
			tot.Deficits++
		}
		lignes = append(lignes, l)
	}
	return lignes, tot, nil
}

func premiereErreurEvaluation(errs ...error) error {
	for _, e := range errs {
		if e != nil {
			return e
		}
	}
	return nil
}

// coutsServeurs somme prix fournisseur et coût annuel du modèle de la
// révision rattachée à aDate, pour les serveurs donnés. Un serveur sans
// révision à cette date ne compte pas.
func (s *serveur) coutsServeurs(ids []int64, aDate string) (float64, float64, error) {
	if len(ids) == 0 {
		return 0, 0, nil
	}
	marques := make([]string, len(ids))
	args := []any{aDate, aDate}
	for i, id := range ids {
		marques[i] = "?"
		args = append(args, id)
	}
	var acquisition, annuel float64
	err := s.depot.Base().QueryRow(
		`SELECT COALESCE(SUM(m.prix_fournisseur_ht), 0), COALESCE(SUM(m.cout_annuel_ht), 0)
		 FROM serveur_revision sr
		 JOIN revision rev ON rev.id = sr.revision_id
		 JOIN modele m ON m.id = rev.modele_id
		 WHERE sr.date_debut <= ? AND (sr.date_fin IS NULL OR sr.date_fin >= ?)
		   AND sr.serveur_id IN (`+strings.Join(marques, ",")+`)`, args...).Scan(&acquisition, &annuel)
	return acquisition, annuel, err
}

func tableauSynthese(sc depot.Scenario, annee int, lignes []ligneSynthese) tableau {
	t := tableau{Titre: fmt.Sprintf("Synthèse — %s — %d", sc.Nom, annee), Colonnes: []string{
		"Cluster", "Techno", "Tier", "Règle limitante", "Composant",
		"Besoin réel", "Offre réelle", "Écart réel", "Besoin scénario", "Offre scénario", "Écart scénario",
		"Serveurs réel", "Serveurs scénario", "Arrivées", "Départs", "Coût d'acquisition", "Coût annuel", "Erreur",
	}}
	for _, l := range lignes {
		if l.Erreur != "" || l.SansRegle {
			erreur := l.Erreur
			if l.SansRegle {
				erreur = "aucune règle active"
			}
			t.Lignes = append(t.Lignes, []any{l.Cluster.Nom, l.TechnoLibelle, l.TierLibelle, nil, nil, nil, nil, nil, nil, nil, nil,
				l.NbAvant, l.NbApres, l.Arrivees, l.Departs, l.CoutAcquisition, l.CoutAnnuel, erreur})
			continue
		}
		t.Lignes = append(t.Lignes, []any{l.Cluster.Nom, l.TechnoLibelle, l.TierLibelle, l.Regle, l.Composant,
			l.Avant.Besoin, l.Avant.Offre, l.Avant.Ecart, l.Apres.Besoin, l.Apres.Offre, l.Apres.Ecart,
			l.NbAvant, l.NbApres, l.Arrivees, l.Departs, l.CoutAcquisition, l.CoutAnnuel, nil})
	}
	return t
}

func (s *serveur) scenarioSynthesePage(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.lireScenarioChemin(w, r)
	if !ok {
		return
	}
	annee := anneeDepuisRequete(r)
	lignes, totaux, err := s.calculerSynthese(sc, annee)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	if s.exporterNomme(w, r, "synthese", func() (tableau, error) {
		return tableauSynthese(sc, annee, lignes), nil
	}) {
		return
	}
	libelles, err := s.chargerLibellesProjets()
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	s.rendrePage(w, r, s.titre(r, "titre.synthese")+" — "+sc.Nom, "scenario_synthese_page", map[string]any{
		"Scenario":        scenarioEnLigne(sc, libelles),
		"Annee":           annee,
		"DateOffre":       dateReferenceOffre(annee),
		"Lignes":          lignes,
		"Totaux":          totaux,
		"Ouvert":          sc.Statut == depot.ScenarioBrouillon || sc.Statut == depot.ScenarioActif,
		"LienComparaison": lienComparaisonScenario(sc.ID, annee),
		"Export":          liensExportPour(r, "synthese"),
	})
}

// ------------------------------------------------------------------ lot

// entreeLot est ce que la page, l'aperçu et la matérialisation lisent tous
// trois de la requête : l'année, les générations conservées (global), le
// modèle candidat par tier et par cluster. Les deux derniers voyagent en
// champs multi-valeurs « <id>:<modèle> » (modele_tier, modele_cluster) :
// un nom de champ fixe, que liensExportVers peut reprendre tel quel.
type entreeLot struct {
	Scenario      depot.Scenario
	Annee         int
	DateOffre     string
	Conserver     map[int64]bool
	ModeleParTier map[int64]int64 // 0 : clusters sans tier
	ModeleParClus map[int64]int64
}

func (s *serveur) lireEntreeLot(r *http.Request, sc depot.Scenario) entreeLot {
	_ = r.ParseForm()
	e := entreeLot{
		Scenario: sc, Annee: anneeDepuisRequete(r),
		Conserver: map[int64]bool{}, ModeleParTier: map[int64]int64{}, ModeleParClus: map[int64]int64{},
	}
	e.DateOffre = dateReferenceOffre(e.Annee)
	for _, v := range r.Form["conserver"] {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			e.Conserver[n] = true
		}
	}
	lireCouples := func(nom string, cible map[int64]int64) {
		for _, v := range r.Form[nom] {
			cle, val, ok := strings.Cut(v, ":")
			if !ok {
				continue
			}
			k, err1 := strconv.ParseInt(cle, 10, 64)
			m, err2 := strconv.ParseInt(val, 10, 64)
			if err1 == nil && err2 == nil && m > 0 {
				cible[k] = m
			}
		}
	}
	lireCouples("modele_tier", e.ModeleParTier)
	lireCouples("modele_cluster", e.ModeleParClus)
	return e
}

// modelePour résout le modèle candidat d'un cluster : son choix propre,
// sinon celui de son tier, sinon rien (le cluster est laissé de côté —
// jamais un modèle choisi à la place de l'utilisateur).
func (e entreeLot) modelePour(c depot.Cluster) int64 {
	if m := e.ModeleParClus[c.ID]; m > 0 {
		return m
	}
	return e.ModeleParTier[entierOuZero(c.TierID)]
}

// clusterLot est une ligne de la table de choix de la page du lot.
type clusterLot struct {
	depot.Cluster
	TierLibelle string
	NbInstalles int
}

type tierLot struct {
	ID      int64 // 0 : sans tier
	Libelle string
}

// chargerParcLot lit les serveurs installés de chaque cluster du scénario
// à la date de référence — la matière commune de la page (générations à
// conserver), de l'aperçu et de la matérialisation.
func (s *serveur) chargerParcLot(sc depot.Scenario, aDate string) ([]depot.Cluster, map[int64][]serveurInstalle, error) {
	clusters, err := s.clustersDuScenario(sc)
	if err != nil {
		return nil, nil, err
	}
	parc := map[int64][]serveurInstalle{}
	for _, c := range clusters {
		installes, err := s.chargerInstalles(c.ID, &sc.ID, aDate)
		if err != nil {
			return nil, nil, err
		}
		parc[c.ID] = installes
	}
	return clusters, parc, nil
}

func (s *serveur) scenarioLotPage(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.lireScenarioChemin(w, r)
	if !ok {
		return
	}
	e := s.lireEntreeLot(r, sc)
	clusters, parc, err := s.chargerParcLot(sc, e.DateOffre)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	if s.exporterNomme(w, r, "apercu", func() (tableau, error) {
		lignes, _, err := s.calculerLot(e, clusters, parc)
		if err != nil {
			return tableau{}, err
		}
		return tableauLot(sc, e.Annee, lignes), nil
	}) {
		return
	}
	lc, err := s.chargerLibellesCluster()
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	modeles, err := s.depot.ListerModeles(false)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}

	// générations installées, tous clusters confondus, toutes conservées par
	// défaut : le lot est d'abord un renfort, retirer une génération est un
	// geste explicite.
	var tous []serveurInstalle
	for _, c := range clusters {
		tous = append(tous, parc[c.ID]...)
	}
	generations := generations(tous, nil, true)

	lignes := make([]clusterLot, 0, len(clusters))
	tiersVus := map[int64]bool{}
	var tiers []tierLot
	for _, c := range clusters {
		l := clusterLot{Cluster: c, NbInstalles: len(parc[c.ID])}
		tid := entierOuZero(c.TierID)
		if c.TierID != nil {
			l.TierLibelle = lc.Tiers[*c.TierID]
		}
		if !tiersVus[tid] {
			tiersVus[tid] = true
			tiers = append(tiers, tierLot{ID: tid, Libelle: l.TierLibelle})
		}
		lignes = append(lignes, l)
	}
	sort.SliceStable(tiers, func(i, j int) bool { return tiers[i].Libelle < tiers[j].Libelle })

	s.rendrePage(w, r, s.titre(r, "titre.lot")+" — "+sc.Nom, "scenario_lot_page", map[string]any{
		"Scenario":    sc,
		"Annee":       e.Annee,
		"DateOffre":   e.DateOffre,
		"Ouvert":      sc.Statut == depot.ScenarioBrouillon || sc.Statut == depot.ScenarioActif,
		"Generations": generations,
		"NbInstalles": len(tous),
		"Tiers":       tiers,
		"Clusters":    lignes,
		"Modeles":     modeles,
	})
}

// ligneLot est un cluster dans l'aperçu du lot : le résultat du
// dimensionnement inverse pour le modèle résolu, ou la raison pour laquelle
// rien ne sera posé.
type ligneLot struct {
	Cluster     depot.Cluster
	TierLibelle string
	Modele      string
	Candidat    ligneCandidat
	NbConserves int
	NbRetires   int
	Erreur      string // évaluation impossible : ligne exclue du lot
	SansModele  bool   // aucun modèle choisi : ligne exclue du lot
	// RienAFaire : modèle choisi, calcul possible, mais 0 à ajouter et 0 à
	// retirer — ligne exclue du lot (Materialiser refuserait un vide)
	RienAFaire bool
}

func (l ligneLot) Actionnable() bool { return l.Erreur == "" && !l.SansModele && !l.RienAFaire }

type totauxLot struct {
	Clusters, AAjouter, ARetirer int
	CoutAcquisition, CoutAnnuel  float64
}

// calculerLot enchaîne le dimensionnement inverse (calculerCandidats, v2.3)
// sur chaque cluster avec son modèle résolu et les générations conservées
// globales — exactement ce que l'écran d'un cluster calcule, cluster par
// cluster.
func (s *serveur) calculerLot(e entreeLot, clusters []depot.Cluster, parc map[int64][]serveurInstalle) ([]ligneLot, totauxLot, error) {
	lc, err := s.chargerLibellesCluster()
	if err != nil {
		return nil, totauxLot{}, err
	}
	var lignes []ligneLot
	var tot totauxLot
	for _, c := range clusters {
		l := ligneLot{Cluster: c}
		if c.TierID != nil {
			l.TierLibelle = lc.Tiers[*c.TierID]
		}
		modeleID := e.modelePour(c)
		if modeleID == 0 {
			l.SansModele = true
			lignes = append(lignes, l)
			continue
		}
		installes := parc[c.ID]
		for _, si := range installes {
			if e.Conserver[si.ModeleID] {
				l.NbConserves++
			} else {
				l.NbRetires++
			}
		}
		candidats, err := s.calculerCandidats(entreeDimensionnement{
			Cluster: c, Annee: e.Annee, ScenarioID: &e.Scenario.ID, DateOffre: e.DateOffre,
			Conserver: e.Conserver, Candidats: []int64{modeleID},
		}, installes)
		if err != nil {
			return nil, totauxLot{}, err
		}
		if len(candidats) != 1 {
			l.Erreur = "modèle candidat introuvable"
			lignes = append(lignes, l)
			continue
		}
		l.Candidat = candidats[0]
		l.Modele = l.Candidat.Code
		if l.Candidat.Erreur != "" {
			l.Erreur = l.Candidat.Erreur
			lignes = append(lignes, l)
			continue
		}
		if l.Candidat.NbServeurs == 0 && l.NbRetires == 0 {
			l.RienAFaire = true
			lignes = append(lignes, l)
			continue
		}
		tot.Clusters++
		tot.AAjouter += l.Candidat.NbServeurs
		tot.ARetirer += l.NbRetires
		tot.CoutAcquisition += l.Candidat.CoutAcquisition
		tot.CoutAnnuel += l.Candidat.CoutAnnuel
		lignes = append(lignes, l)
	}
	return lignes, tot, nil
}

func tableauLot(sc depot.Scenario, annee int, lignes []ligneLot) tableau {
	t := tableau{Titre: fmt.Sprintf("Dimensionnement en lot — %s — %d", sc.Nom, annee), Colonnes: []string{
		"Cluster", "Tier", "Modèle", "À ajouter", "Par zone", "Règle limitante", "Conservés", "À retirer",
		"Coût d'acquisition", "Coût annuel", "Exclu",
	}}
	for _, l := range lignes {
		motif := ""
		switch {
		case l.SansModele:
			motif = "aucun modèle choisi"
		case l.Erreur != "":
			motif = l.Erreur
		case l.RienAFaire:
			motif = "rien à faire"
		}
		if l.SansModele || l.Erreur != "" {
			t.Lignes = append(t.Lignes, []any{l.Cluster.Nom, l.TierLibelle, l.Modele, nil, nil, nil, l.NbConserves, l.NbRetires, nil, nil, motif})
			continue
		}
		var parZone any
		if l.Candidat.ParZone != nil {
			parZone = *l.Candidat.ParZone
		}
		t.Lignes = append(t.Lignes, []any{l.Cluster.Nom, l.TierLibelle, l.Modele, l.Candidat.NbServeurs, parZone,
			l.Candidat.RegleLimitante, l.NbConserves, l.NbRetires, l.Candidat.CoutAcquisition, l.Candidat.CoutAnnuel, motif})
	}
	return t
}

func exportLot(r *http.Request, scenarioID int64) exportLiens {
	return liensExportVers(r, fmt.Sprintf("/scenarios/%d/lot", scenarioID), "apercu",
		"annee", "conserver", "modele_tier", "modele_cluster")
}

func (s *serveur) scenarioLotApercu(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.lireScenarioChemin(w, r)
	if !ok {
		return
	}
	s.rendreApercuLot(w, r, s.lireEntreeLot(r, sc), "")
}

func (s *serveur) rendreApercuLot(w http.ResponseWriter, r *http.Request, e entreeLot, erreur string) {
	clusters, parc, err := s.chargerParcLot(e.Scenario, e.DateOffre)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	lignes, totaux, err := s.calculerLot(e, clusters, parc)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	s.rendreFragment(w, r, "scenario_lot_apercu", map[string]any{
		"Scenario": e.Scenario,
		"Annee":    e.Annee,
		"Lignes":   lignes,
		"Totaux":   totaux,
		"Erreur":   erreur,
		"Export":   exportLot(r, e.Scenario.ID),
	})
}

// zonesMaterialisation : les zones déjà occupées par le cluster, à défaut
// toutes ; limitées au nombre imposé par les contraintes s'il y en a un —
// la même règle que la matérialisation d'un cluster.
func (s *serveur) zonesMaterialisation(installes []serveurInstalle, nbZones *int) ([]int64, error) {
	var zones []int64
	vus := map[int64]bool{}
	for _, si := range installes {
		if si.ZoneID != nil && !vus[*si.ZoneID] {
			vus[*si.ZoneID] = true
			zones = append(zones, *si.ZoneID)
		}
	}
	if len(zones) == 0 {
		toutes, err := s.depot.ListerZones()
		if err != nil {
			return nil, err
		}
		for _, z := range toutes {
			zones = append(zones, z.ID)
		}
	}
	sort.Slice(zones, func(i, j int) bool { return zones[i] < zones[j] })
	if nbZones != nil && *nbZones > 0 && *nbZones < len(zones) {
		zones = zones[:*nbZones]
	}
	return zones, nil
}

// scenarioLotMaterialiser recalcule tout depuis le formulaire (jamais
// confiance à un nombre affiché) et pose, en une transaction, une
// matérialisation par cluster actionnable. Succès : redirection vers la
// synthèse, où l'avant / après montre aussitôt le résultat.
func (s *serveur) scenarioLotMaterialiser(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.lireScenarioChemin(w, r)
	if !ok {
		return
	}
	e := s.lireEntreeLot(r, sc)
	clusters, parc, err := s.chargerParcLot(sc, e.DateOffre)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	lignes, _, err := s.calculerLot(e, clusters, parc)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}

	var lot []depot.Materialisation
	for _, l := range lignes {
		if !l.Actionnable() {
			continue
		}
		installes := parc[l.Cluster.ID]
		zones, err := s.zonesMaterialisation(installes, l.Candidat.NbZones)
		if err != nil {
			s.erreurServeur(w, r, err)
			return
		}
		var retirer []int64
		for _, si := range installes {
			if !e.Conserver[si.ModeleID] {
				retirer = append(retirer, si.ID)
			}
		}
		lot = append(lot, depot.Materialisation{
			ScenarioID: sc.ID, ClusterID: l.Cluster.ID, RevisionID: l.Candidat.RevisionID,
			Nb: l.Candidat.NbServeurs, DateDebut: fmt.Sprintf("%04d-01-01", e.Annee),
			Prefixe: "HYP-" + l.Cluster.Nom + "-" + l.Candidat.Code, Zones: zones, Retirer: retirer,
		})
	}
	if len(lot) == 0 {
		s.rendreApercuLot(w, r, e, "Rien à matérialiser : aucun cluster avec un modèle choisi et quelque chose à poser.")
		return
	}
	if _, err := s.depotPour(r).MaterialiserLot(lot); err != nil {
		if !erreurMetier(err) {
			s.erreurServeur(w, r, err)
			return
		}
		s.rendreApercuLot(w, r, e, messageUtilisateur(err))
		return
	}

	cible := fmt.Sprintf("/scenarios/%d/synthese?annee=%d", sc.ID, e.Annee)
	if estRequeteHTMX(r) {
		w.Header().Set("HX-Redirect", cible)
		w.WriteHeader(http.StatusOK)
		return
	}
	http.Redirect(w, r, cible, http.StatusSeeOther)
}
