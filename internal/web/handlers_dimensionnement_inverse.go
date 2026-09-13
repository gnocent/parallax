package web

import (
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"parallax/internal/capacity"
	"parallax/internal/depot"
)

// routesDimensionnementInverse : l'écran de dimensionnement inverse
// multi-modèles (v2.3 du backlog). Pour un cluster, une année et un scénario,
// l'utilisateur choisit ce qu'il CONSERVE du parc installé (par génération,
// c'est-à-dire par modèle — décision de cadrage : ni tout, ni rien, une
// sélection) et les modèles candidats (dans leur dernière révision), et
// obtient pour chaque candidat le nombre de serveurs à ajouter, la règle
// limitante, les deux coûts, la capacité obtenue et le surplus. Le choix se
// matérialise en serveurs hypothétiques dans le scénario, les non conservés
// étant retirés du cluster (depot.Materialiser, une transaction).
//
// Le moteur est capacity.Dimensionner, avec OffreConservee = capacité des
// serveurs conservés (étape 1b de l'ordre de dimensionnement, CLAUDE.md).
func (s *serveur) routesDimensionnementInverse() {
	s.mux.HandleFunc("GET /clusters/{id}/dimensionnement", s.lecteur(s.dimensionnementPage))
	s.mux.HandleFunc("GET /clusters/{id}/dimensionnement/resultat", s.lecteur(s.dimensionnementResultat))
	s.mux.HandleFunc("POST /clusters/{id}/dimensionnement/materialiser", s.editeur(s.dimensionnementMaterialiser))
}

// serveurInstalle est un serveur occupant effectivement le cluster à la date
// de référence, avec sa génération (modèle de la révision rattachée à cette
// date) et ses capacités par code de composant.
type serveurInstalle struct {
	ID         int64
	ModeleID   int64 // 0 : aucune révision rattachée à la date
	ModeleCode string
	ZoneID     *int64
	Capacites  map[string]float64
}

// generationInstallee regroupe les serveurs installés par modèle, pour la
// case « conserver » de l'écran.
type generationInstallee struct {
	ModeleID  int64
	Code      string
	Nb        int
	Capacites string // "ssd=1152, cpu=80" — lisible, pas calculable
	Conservee bool
}

type candidatModele struct {
	depot.Modele
	Coche bool
}

// ligneCandidat est une ligne du tableau comparatif.
type ligneCandidat struct {
	ModeleID          int64
	Code              string
	RevisionID        int64
	RevisionNumero    int64
	Erreur            string // modèle sans révision, sans le composant d'une règle…
	NbServeurs        int
	ParZone           *int
	NbZones           *int
	RegleLimitante    string
	ComposantLimitant string
	CapaciteUnitaire  float64
	CapaciteObtenue   float64 // nb x capacité unitaire du composant limitant
	CapaciteConservee float64 // offre conservée sur ce composant
	Besoin            float64 // besoin brut de la règle limitante
	Surplus           float64 // conservée + obtenue − besoin
	CoutAcquisition   float64
	CoutAnnuel        float64
	Details           []capacity.DetailRegle
}

func (l ligneCandidat) ParZoneTexte() string {
	if l.ParZone == nil {
		return "—"
	}
	return strconv.Itoa(*l.ParZone)
}

// entreeDimensionnement rassemble ce que la page et le fragment lisent tous
// deux de la requête.
type entreeDimensionnement struct {
	Cluster    depot.Cluster
	Annee      int
	ScenarioID *int64
	DateOffre  string
	Conserver  map[int64]bool // modèles conservés (0 = serveurs sans révision)
	Candidats  []int64
}

func (s *serveur) lireEntreeDimensionnement(r *http.Request) (entreeDimensionnement, error) {
	id, err := idChemin(r)
	if err != nil {
		return entreeDimensionnement{}, err
	}
	cluster, err := s.depot.LireCluster(id)
	if err != nil {
		return entreeDimensionnement{}, err
	}
	_ = r.ParseForm()
	e := entreeDimensionnement{
		Cluster: cluster, Annee: anneeDepuisRequete(r), ScenarioID: scenarioDepuisRequete(r),
		Conserver: map[int64]bool{},
	}
	e.DateOffre = dateReferenceOffre(e.Annee)
	for _, v := range r.Form["conserver"] {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			e.Conserver[n] = true
		}
	}
	for _, v := range r.Form["candidat"] {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			e.Candidats = append(e.Candidats, n)
		}
	}
	return e, nil
}

// chargerInstalles renvoie les serveurs occupant effectivement le cluster à
// la date de référence, sous le scénario, avec génération et capacités.
func (s *serveur) chargerInstalles(clusterID int64, scenarioID *int64, aDate string) ([]serveurInstalle, error) {
	affectations, err := s.depot.AffectationsResolues(clusterID, scenarioID, aDate)
	if err != nil {
		return nil, err
	}
	if len(affectations) == 0 {
		return nil, nil
	}
	parID := map[int64]*serveurInstalle{}
	var ordre []int64
	marques := make([]string, len(affectations))
	args := []any{aDate, aDate}
	for i, a := range affectations {
		parID[a.ServeurID] = &serveurInstalle{ID: a.ServeurID, Capacites: map[string]float64{}}
		ordre = append(ordre, a.ServeurID)
		marques[i] = "?"
		args = append(args, a.ServeurID)
	}

	lignes, err := s.depot.Base().Query(
		`SELECT s.id, s.zone_id, COALESCE(m.id, 0), COALESCE(m.code, ''),
		        COALESCE(comp.code, ''), COALESCE(comp.quantite * comp.capacite_unitaire, 0)
		 FROM serveur s
		 LEFT JOIN serveur_revision sr ON sr.serveur_id = s.id
		      AND sr.date_debut <= ? AND (sr.date_fin IS NULL OR sr.date_fin >= ?)
		 LEFT JOIN revision rev ON rev.id = sr.revision_id
		 LEFT JOIN modele m ON m.id = rev.modele_id
		 LEFT JOIN composant comp ON comp.revision_id = sr.revision_id
		 WHERE s.id IN (`+strings.Join(marques, ",")+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer lignes.Close()
	for lignes.Next() {
		var id, modeleID int64
		var dc *int64
		var modeleCode, code string
		var capa float64
		if err := lignes.Scan(&id, &dc, &modeleID, &modeleCode, &code, &capa); err != nil {
			return nil, err
		}
		si := parID[id]
		si.ModeleID, si.ModeleCode, si.ZoneID = modeleID, modeleCode, dc
		if code != "" {
			si.Capacites[code] += capa
		}
	}
	if err := lignes.Err(); err != nil {
		return nil, err
	}

	out := make([]serveurInstalle, 0, len(ordre))
	for _, id := range ordre {
		out = append(out, *parID[id])
	}
	return out, nil
}

func generations(installes []serveurInstalle, conserver map[int64]bool, defautTout bool) []generationInstallee {
	index := map[int64]*generationInstallee{}
	capas := map[int64]map[string]float64{}
	var ordre []int64
	for _, si := range installes {
		g, ok := index[si.ModeleID]
		if !ok {
			code := si.ModeleCode
			if si.ModeleID == 0 {
				code = "(sans révision rattachée)"
			}
			g = &generationInstallee{ModeleID: si.ModeleID, Code: code}
			index[si.ModeleID] = g
			capas[si.ModeleID] = map[string]float64{}
			ordre = append(ordre, si.ModeleID)
		}
		g.Nb++
		for c, v := range si.Capacites {
			capas[si.ModeleID][c] += v
		}
	}
	sort.Slice(ordre, func(i, j int) bool { return index[ordre[i]].Code < index[ordre[j]].Code })

	out := make([]generationInstallee, 0, len(ordre))
	for _, id := range ordre {
		g := index[id]
		g.Conservee = defautTout || conserver[id]
		var parts []string
		codes := make([]string, 0, len(capas[id]))
		for c := range capas[id] {
			codes = append(codes, c)
		}
		sort.Strings(codes)
		for _, c := range codes {
			parts = append(parts, c+"="+formaterNombre(capas[id][c]))
		}
		g.Capacites = strings.Join(parts, ", ")
		out = append(out, *g)
	}
	return out
}

// derniereRevision renvoie la révision de plus grand numéro d'un modèle et
// son offre (code de composant → capacité totale).
func (s *serveur) derniereRevision(modeleID int64) (depot.Revision, map[string]float64, error) {
	revisions, err := s.depot.ListerRevisions(modeleID)
	if err != nil {
		return depot.Revision{}, nil, err
	}
	if len(revisions) == 0 {
		return depot.Revision{}, nil, fmt.Errorf("%w : aucune révision", depot.ErrValidation)
	}
	derniere := revisions[0]
	for _, r := range revisions[1:] {
		if r.Numero > derniere.Numero {
			derniere = r
		}
	}
	composants, err := s.depot.ListerComposants(derniere.ID)
	if err != nil {
		return depot.Revision{}, nil, err
	}
	offre := map[string]float64{}
	for _, c := range composants {
		offre[c.Code] += c.Quantite * c.CapaciteUnitaire
	}
	return derniere, offre, nil
}

// calculerCandidats dimensionne chaque modèle candidat pour le besoin
// résiduel du cluster une fois l'offre conservée déduite.
func (s *serveur) calculerCandidats(e entreeDimensionnement, installes []serveurInstalle) ([]ligneCandidat, error) {
	regles, err := s.depot.ReglesCapaciteActives(0)
	if err != nil {
		return nil, err
	}
	valeurs, err := s.chargerValeursAnnee(e.Annee, e.ScenarioID)
	if err != nil {
		return nil, err
	}
	defauts, err := s.chargerDefautsVariables()
	if err != nil {
		return nil, err
	}
	contraintes, err := s.depot.ContraintesResolues(e.Cluster.ID, e.Annee, e.ScenarioID)
	if err != nil {
		return nil, err
	}

	// offre conservée, agrégats et serveurs (règles PAR_SERVEUR) : sur les
	// seuls serveurs conservés — ce sont eux qui restent dans le périmètre.
	conservee := map[string]float64{}
	var serveurs []map[string]float64
	for _, si := range installes {
		if !e.Conserver[si.ModeleID] {
			continue
		}
		machine := map[string]float64{}
		for c, v := range si.Capacites {
			conservee[c] += v
			machine[c+"_machine"] = v
		}
		serveurs = append(serveurs, machine)
	}
	agregats := map[string]float64{"nb_serveurs": float64(len(serveurs))}
	for c, v := range conservee {
		agregats[c+"_total"] = v
		if _, ok := defauts[c+"_machine"]; !ok {
			defauts[c+"_machine"] = 0
		}
		if _, ok := defauts[c+"_total"]; !ok {
			defauts[c+"_total"] = 0
		}
	}
	capaCluster := capacity.Cluster{
		ID: e.Cluster.ID, ProjetID: &e.Cluster.ProjetID, EnvironnementID: &e.Cluster.EnvironnementID,
		TechnoID: &e.Cluster.TechnoID, TierID: e.Cluster.TierID, UsageID: e.Cluster.UsageFonctionnelID,
	}

	var lignes []ligneCandidat
	for _, modeleID := range e.Candidats {
		m, err := s.depot.LireModele(modeleID)
		if err != nil {
			if errors.Is(err, depot.ErrIntrouvable) {
				continue
			}
			return nil, err
		}
		l := ligneCandidat{ModeleID: m.ID, Code: m.Code}
		rev, offre, err := s.derniereRevision(m.ID)
		if err != nil {
			if !erreurMetier(err) {
				return nil, err
			}
			l.Erreur = messageUtilisateur(err)
			lignes = append(lignes, l)
			continue
		}
		l.RevisionID, l.RevisionNumero = rev.ID, rev.Numero

		candidat := capacity.Modele{Code: m.Code, Composants: offre}
		if m.PrixFournisseurHT != nil {
			candidat.PrixFournisseurHT = *m.PrixFournisseurHT
		}
		if m.CoutAnnuelHT != nil {
			candidat.CoutAnnuelHT = *m.CoutAnnuelHT
		}
		// les composants de ce candidat ont aussi besoin d'un défaut pour les
		// variables "<code>_machine"/"<code>_total" éventuellement référencées.
		for c := range offre {
			if _, ok := defauts[c+"_machine"]; !ok {
				defauts[c+"_machine"] = 0
			}
			if _, ok := defauts[c+"_total"]; !ok {
				defauts[c+"_total"] = 0
			}
		}

		dim, err := capacity.Dimensionner(capacity.EntreeDimensionnement{
			Cluster: capaCluster, Regles: regles, Valeurs: valeurs, Defauts: defauts,
			Annee: e.Annee, ScenarioID: e.ScenarioID, Modele: candidat, Contraintes: contraintes,
			OffreConservee: conservee, Agregats: agregats, Serveurs: serveurs,
		})
		if err != nil {
			l.Erreur = err.Error()
			lignes = append(lignes, l)
			continue
		}
		l.NbServeurs, l.ParZone, l.NbZones = dim.NbServeurs, dim.ParZone, dim.NbZones
		l.CoutAcquisition, l.CoutAnnuel, l.Details = dim.CoutAcquisition, dim.CoutAnnuel, dim.Details
		if dim.RegleLimitante != nil {
			for _, d := range dim.Details {
				if d.RegleID == *dim.RegleLimitante {
					l.RegleLimitante = d.Nom
					l.Besoin = d.Besoin
					l.CapaciteUnitaire = d.CapaciteUnitaire
					for _, r := range regles {
						if r.ID == d.RegleID {
							l.ComposantLimitant = r.ComposantOffre
						}
					}
				}
			}
			l.CapaciteObtenue = float64(l.NbServeurs) * l.CapaciteUnitaire
			l.CapaciteConservee = conservee[l.ComposantLimitant]
			l.Surplus = l.CapaciteConservee + l.CapaciteObtenue - l.Besoin
		} else {
			l.Erreur = "aucune règle active ne s'applique à ce cluster"
		}
		lignes = append(lignes, l)
	}
	return lignes, nil
}

// ------------------------------------------------------------------ pages

// exportDimensionnement construit les liens d'export du tableau comparatif
// depuis le fragment résultat : vers la page, en reprenant année, scénario,
// générations conservées et candidats cochés — exactement l'entrée que
// lireEntreeDimensionnement relit pour recalculer (voir liensExportVers).
func exportDimensionnement(r *http.Request, clusterID int64) exportLiens {
	return liensExportVers(r, fmt.Sprintf("/clusters/%d/dimensionnement", clusterID), "resultat",
		"annee", "scenario", "conserver", "candidat")
}

// tableauGenerations est la forme exportable du parc installé par génération
// — sans la case « conserver », qui est une saisie, pas une donnée.
func tableauGenerations(cluster depot.Cluster, gens []generationInstallee) tableau {
	t := tableau{Titre: "Parc installé — " + cluster.Nom, Colonnes: []string{"Génération", "Nb", "Capacités cumulées"}}
	for _, g := range gens {
		t.Lignes = append(t.Lignes, []any{g.Code, g.Nb, g.Capacites})
	}
	return t
}

// tableauCandidats est la forme exportable du tableau comparatif : une ligne
// par candidat, ses chiffres bruts, et son erreur éventuelle (modèle sans
// révision, règle sans composant…) dans une colonne dédiée plutôt qu'une
// ligne muette.
func tableauCandidats(cluster depot.Cluster, annee int, lignes []ligneCandidat) tableau {
	t := tableau{Titre: fmt.Sprintf("Dimensionnement — %s — %d", cluster.Nom, annee), Colonnes: []string{
		"Modèle", "Révision", "À ajouter", "Par zone", "Règle limitante", "Composant limitant",
		"Besoin", "Conservé", "Obtenu", "Surplus", "Coût d'acquisition", "Coût annuel", "Erreur",
	}}
	for _, l := range lignes {
		if l.Erreur != "" {
			t.Lignes = append(t.Lignes, []any{l.Code, l.RevisionNumero, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, l.Erreur})
			continue
		}
		var parZone any
		if l.ParZone != nil {
			parZone = *l.ParZone
		}
		t.Lignes = append(t.Lignes, []any{
			l.Code, l.RevisionNumero, l.NbServeurs, parZone, l.RegleLimitante, l.ComposantLimitant,
			l.Besoin, l.CapaciteConservee, l.CapaciteObtenue, l.Surplus, l.CoutAcquisition, l.CoutAnnuel, nil,
		})
	}
	return t
}

func (s *serveur) dimensionnementPage(w http.ResponseWriter, r *http.Request) {
	e, err := s.lireEntreeDimensionnement(r)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	installes, err := s.chargerInstalles(e.Cluster.ID, e.ScenarioID, e.DateOffre)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	// export universel (tableau.go), deux tableaux nommés : le parc installé
	// par génération, et le tableau comparatif recalculé depuis les
	// paramètres que le fragment résultat a mis dans ses liens.
	if s.exporterNomme(w, r, "generations", func() (tableau, error) {
		return tableauGenerations(e.Cluster, generations(installes, nil, true)), nil
	}) {
		return
	}
	if s.exporterNomme(w, r, "resultat", func() (tableau, error) {
		lignes, err := s.calculerCandidats(e, installes)
		if err != nil {
			return tableau{}, err
		}
		return tableauCandidats(e.Cluster, e.Annee, lignes), nil
	}) {
		return
	}
	modeles, err := s.depot.ListerModeles(false)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	selecteur, err := s.chargerSelecteurScenarios(r)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}

	// candidats cochés par défaut : les modèles de l'année courante et de la
	// suivante — le cas général d'une hypothèse (décision de cadrage).
	anneeCourante := int64(time.Now().Year())
	candidats := make([]candidatModele, 0, len(modeles))
	for _, m := range modeles {
		candidats = append(candidats, candidatModele{
			Modele: m, Coche: m.Annee == anneeCourante || m.Annee == anneeCourante+1,
		})
	}

	s.rendrePage(w, r, s.titre(r, "titre.dimensionnement")+" — "+e.Cluster.Nom, "cluster_dimensionnement_page", map[string]any{
		"Cluster":           e.Cluster,
		"Annee":             e.Annee,
		"DateOffre":         e.DateOffre,
		"Scenarios":         selecteur.Scenarios,
		"ScenarioID":        selecteur.ScenarioActifID,
		"Generations":       generations(installes, nil, true),
		"NbInstalles":       len(installes),
		"Candidats":         candidats,
		"ExportGenerations": liensExportPour(r, "generations"),
	})
}

func (s *serveur) dimensionnementResultat(w http.ResponseWriter, r *http.Request) {
	e, err := s.lireEntreeDimensionnement(r)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	s.rendreResultatDimensionnement(w, r, e, "")
}

func (s *serveur) rendreResultatDimensionnement(w http.ResponseWriter, r *http.Request, e entreeDimensionnement, erreur string) {
	installes, err := s.chargerInstalles(e.Cluster.ID, e.ScenarioID, e.DateOffre)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	lignes, err := s.calculerCandidats(e, installes)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	nbRetires := 0
	for _, si := range installes {
		if !e.Conserver[si.ModeleID] {
			nbRetires++
		}
	}
	var scenarioIDOuZero int64
	if e.ScenarioID != nil {
		scenarioIDOuZero = *e.ScenarioID
	}
	s.rendreFragment(w, r, "cluster_dimensionnement_resultat", map[string]any{
		"Cluster":     e.Cluster,
		"Annee":       e.Annee,
		"ScenarioID":  scenarioIDOuZero,
		"Lignes":      lignes,
		"NbRetires":   nbRetires,
		"NbConserves": len(installes) - nbRetires,
		"Erreur":      erreur,
		"Export":      exportDimensionnement(r, e.Cluster.ID),
	})
}

// dimensionnementMaterialiser recalcule le candidat choisi (jamais confiance
// à un nombre venu du formulaire) puis pose l'hypothèse dans le scénario.
// Succès : redirection htmx vers le besoin/offre sous ce scénario, où le
// delta montre aussitôt arrivées et départs.
func (s *serveur) dimensionnementMaterialiser(w http.ResponseWriter, r *http.Request) {
	e, err := s.lireEntreeDimensionnement(r)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	if e.ScenarioID == nil {
		s.rendreResultatDimensionnement(w, r, e, "Choisissez un scénario : une hypothèse ne se matérialise jamais dans le réel.")
		return
	}
	modeleID := idRequis(r, "modele_id")
	e.Candidats = []int64{modeleID}

	installes, err := s.chargerInstalles(e.Cluster.ID, e.ScenarioID, e.DateOffre)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	lignes, err := s.calculerCandidats(e, installes)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	if len(lignes) != 1 || lignes[0].Erreur != "" {
		msg := "modèle candidat introuvable"
		if len(lignes) == 1 {
			msg = lignes[0].Erreur
		}
		s.rendreResultatDimensionnement(w, r, e, "Matérialisation impossible : "+msg)
		return
	}
	choix := lignes[0]

	// répartition : les zones déjà utilisés par le cluster, à défaut
	// tous ; limités au nombre imposé par les contraintes s'il y en a un.
	var dcs []int64
	vus := map[int64]bool{}
	for _, si := range installes {
		if si.ZoneID != nil && !vus[*si.ZoneID] {
			vus[*si.ZoneID] = true
			dcs = append(dcs, *si.ZoneID)
		}
	}
	if len(dcs) == 0 {
		tous, err := s.depot.ListerZones()
		if err != nil {
			s.erreurServeur(w, r, err)
			return
		}
		for _, dc := range tous {
			dcs = append(dcs, dc.ID)
		}
	}
	sort.Slice(dcs, func(i, j int) bool { return dcs[i] < dcs[j] })
	if choix.NbZones != nil && *choix.NbZones > 0 && *choix.NbZones < len(dcs) {
		dcs = dcs[:*choix.NbZones]
	}

	var retirer []int64
	for _, si := range installes {
		if !e.Conserver[si.ModeleID] {
			retirer = append(retirer, si.ID)
		}
	}

	_, errMat := s.depotPour(r).Materialiser(depot.Materialisation{
		ScenarioID: *e.ScenarioID, ClusterID: e.Cluster.ID, RevisionID: choix.RevisionID,
		Nb: choix.NbServeurs, DateDebut: fmt.Sprintf("%04d-01-01", e.Annee),
		Prefixe: "HYP-" + e.Cluster.Nom + "-" + choix.Code, Zones: dcs, Retirer: retirer,
	})
	if errMat != nil {
		if !erreurMetier(errMat) {
			s.erreurServeur(w, r, errMat)
			return
		}
		s.rendreResultatDimensionnement(w, r, e, messageUtilisateur(errMat))
		return
	}

	cible := fmt.Sprintf("/clusters/%d/besoin-offre?annee=%d&scenario=%d", e.Cluster.ID, e.Annee, *e.ScenarioID)
	if estRequeteHTMX(r) {
		w.Header().Set("HX-Redirect", cible)
		w.WriteHeader(http.StatusOK)
		return
	}
	http.Redirect(w, r, cible, http.StatusSeeOther)
}
