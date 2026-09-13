package web

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"parallax/internal/depot"
)

// routesServeurs enregistre la liste compacte, la page de détail (attributs,
// rattachement de révision, affectation) et la vue « sans affectation
// active ». Lignes à ajouter dans server.go (routes()) : voir le rapport
// d'intégration.
//
// Depuis la v2.1, la section affectation de la page de détail est
// paramétrable par scénario (paramètre "scenario", voir scenario_contexte.go) :
// sous un scénario, Affecter et Réaffecter deviennent tous deux
// depot.AffecterDansScenario (le seau est vierge ou pas, la distinction ne
// change rien côté écran) et Désaffecter devient depot.RetirerDuScenario —
// voir modele-donnees.md §6. Le reste de l'écran (attributs, révision) reste
// toujours réel : seule l'affectation a un sens sous un scénario.
func (s *serveur) routesServeurs() {
	s.mux.HandleFunc("GET /serveurs", s.lecteur(s.serveursPage))
	s.mux.HandleFunc("GET /serveurs/tableau", s.lecteur(s.serveursTableau))
	s.mux.HandleFunc("POST /serveurs", s.editeur(s.serveursCreer))
	s.mux.HandleFunc("GET /serveurs/sans-affectation", s.lecteur(s.serveursSansAffectationPage))
	s.mux.HandleFunc("POST /serveurs/sans-affectation/{id}/affecter", s.editeur(s.serveursSansAffectationAffecter))
	s.mux.HandleFunc("GET /serveurs/revisions-du-modele", s.lecteur(s.serveurRevisionsDuModele))

	s.mux.HandleFunc("GET /serveurs/{id}", s.lecteur(s.serveurDetailPage))
	s.mux.HandleFunc("GET /serveurs/{id}/affectation", s.lecteur(s.serveurAffectationSection))
	s.mux.HandleFunc("PUT /serveurs/{id}", s.editeur(s.serveurAttributsModifier))
	s.mux.HandleFunc("POST /serveurs/{id}/statut", s.editeur(s.serveurStatutChanger))
	s.mux.HandleFunc("POST /serveurs/{id}/revisions", s.editeur(s.serveurRevisionRattacher))
	s.mux.HandleFunc("POST /serveurs/{id}/affecter", s.editeur(s.serveurAffecter))
	s.mux.HandleFunc("POST /serveurs/{id}/reaffecter", s.editeur(s.serveurReaffecter))
	s.mux.HandleFunc("POST /serveurs/{id}/desaffecter", s.editeur(s.serveurDesaffecter))
}

// statutsCreation exclut HYPOTHESE : c'est le statut de changement de statut
// d'un serveur déjà réel (section attributs de la page de détail), où
// HYPOTHESE n'a pas de sens. Le formulaire de création, lui, propose
// statutsCreationTous (ci-dessous) : HYPOTHESE y est valide dès qu'un
// scénario est choisi dans le même formulaire (invariant 5, vérifié par
// CreerServeur).
var statutsCreation = []string{depot.StatutCommande, depot.StatutEnService, depot.StatutDecommissionne}

// statutsCreationTous ajoute HYPOTHESE à statutsCreation, pour le formulaire
// de création de la liste des serveurs (v2.1) — copie plutôt qu'append sur
// statutsCreation, pour ne jamais risquer de partager ou d'écraser son tableau
// support.
var statutsCreationTous = []string{
	depot.StatutCommande, depot.StatutEnService, depot.StatutDecommissionne, depot.StatutHypothese,
}

// ------------------------------------------------------------- liste (v1.3)

// serveurLigne incorpore depot.Serveur, un message d'erreur optionnel (patron
// projetLigne) et le libellé de la zone — la liste est volontairement
// compacte (physical_name, hostname, zone, statut), le détail vit sur
// la page /serveurs/{id}.
type serveurLigne struct {
	depot.Serveur
	Erreur      string
	ZoneLibelle string
	Anomalies   []string // libellés des anomalies réseau du serveur (v3.1), repère sur la ligne
}

func (s *serveur) chargerLibellesZone() (map[int64]string, error) {
	dcs, err := s.depot.ListerZones()
	if err != nil {
		return nil, err
	}
	m := make(map[int64]string, len(dcs))
	for _, dc := range dcs {
		m[dc.ID] = dc.Libelle
	}
	return m, nil
}

func serveurEnLigne(srv depot.Serveur, libellesZone map[int64]string) serveurLigne {
	l := serveurLigne{Serveur: srv}
	if srv.ZoneID != nil {
		l.ZoneLibelle = libellesZone[*srv.ZoneID]
	}
	return l
}

func serveursEnLignes(serveurs []depot.Serveur, libellesZone map[int64]string) []serveurLigne {
	out := make([]serveurLigne, len(serveurs))
	for i, srv := range serveurs {
		out[i] = serveurEnLigne(srv, libellesZone)
	}
	return out
}

// serveurFiltreDepuisRequete lit les filtres statut/zone/scénario,
// préfixés filtre_ pour ne pas entrer en collision avec les champs homonymes
// du formulaire de création (même raison que clusterFiltreDepuisRequete).
//
// filtre_scenario_id (v2.1) : vide = le réel seulement (comportement de la
// v1.3, inchangé) ; un scénario ajoute ses serveurs hypothétiques et ceux du
// réel qu'il touche à la liste (depot.FiltreServeur.ScenarioID, lecture
// brute — voir son doc-comment).
func serveurFiltreDepuisRequete(r *http.Request) (depot.FiltreServeur, error) {
	var f depot.FiltreServeur
	if v := r.FormValue("filtre_statut"); v != "" {
		f.Statut = &v
	}
	dc, err := idOptionnel(r, "filtre_zone_id")
	if err != nil {
		return f, err
	}
	f.ZoneID = dc
	sc, err := idOptionnel(r, "filtre_scenario_id")
	if err != nil {
		return f, err
	}
	f.ScenarioID = sc
	return f, nil
}

// exportServeurs construit les liens d'export du tableau des serveurs à
// partir du filtre effectivement appliqué — pas de la requête : après une
// création sous scénario, serveursCreer force le filtre scénario pour que le
// serveur créé reste visible, et les liens doivent suivre ce même filtre.
// Les boutons vivent dans le fragment "serveurs_tableau" et visent la page
// /serveurs, dont le handler répond (voir exportProjets, handlers_projet.go).
func exportServeurs(f depot.FiltreServeur) exportLiens {
	q := url.Values{}
	if f.Statut != nil {
		q.Set("filtre_statut", *f.Statut)
	}
	if f.ZoneID != nil {
		q.Set("filtre_zone_id", strconv.FormatInt(*f.ZoneID, 10))
	}
	if f.ScenarioID != nil {
		q.Set("filtre_scenario_id", strconv.FormatInt(*f.ScenarioID, 10))
	}
	return liensExportDepuis("/serveurs", q, "")
}

// tableauServeurs est la forme exportable de la liste compacte : les mêmes
// colonnes que l'écran, plus le drapeau « scénario » que le badge affiche.
func tableauServeurs(lignes []serveurLigne) tableau {
	t := tableau{Titre: "Serveurs", Colonnes: []string{"Nom physique", "Hostname", "Zone", "Statut", "Scénario"}}
	for _, l := range lignes {
		t.Lignes = append(t.Lignes, []any{texteOuVide(l.PhysicalName), texteOuVide(l.Hostname), l.ZoneLibelle, l.Statut, l.ScenarioID != nil})
	}
	return t
}

func (s *serveur) serveursPage(w http.ResponseWriter, r *http.Request) {
	// export universel (tableau.go) : les filtres que le fragment a mis dans
	// les liens s'appliquent ici. La page HTML reste affichée sans filtre,
	// comme clustersPage et pour la même raison (ses <select> ne reflètent
	// pas l'URL).
	if s.exporter(w, r, func() (tableau, error) {
		f, err := serveurFiltreDepuisRequete(r)
		if err != nil {
			return tableau{}, err
		}
		serveurs, err := s.depot.ListerServeurs(f)
		if err != nil {
			return tableau{}, err
		}
		libellesZone, err := s.chargerLibellesZone()
		if err != nil {
			return tableau{}, err
		}
		return tableauServeurs(serveursEnLignes(serveurs, libellesZone)), nil
	}) {
		return
	}
	dcs, err := s.depot.ListerZones()
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	serveurs, err := s.depot.ListerServeurs(depot.FiltreServeur{})
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	libellesZone, err := s.chargerLibellesZone()
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	scenarios, err := s.depot.ListerScenarios(false)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	s.rendrePage(w, r, s.titre(r, "titre.serveurs"), "serveurs_page", map[string]any{
		"Serveurs":        serveursEnLignes(serveurs, libellesZone),
		"Zones":           dcs,
		"StatutsCreation": statutsCreationTous,
		"StatutsFiltre":   []string{depot.StatutHypothese, depot.StatutCommande, depot.StatutEnService, depot.StatutDecommissionne},
		"Scenarios":       scenarios,
		"Export":          exportServeurs(depot.FiltreServeur{}),
	})
}

func (s *serveur) rendreServeursTableau(w http.ResponseWriter, r *http.Request, f depot.FiltreServeur, erreur string) {
	serveurs, err := s.depot.ListerServeurs(f)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	libellesZone, err := s.chargerLibellesZone()
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	anomalies, err := s.anomaliesParServeur()
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	lignes := serveursEnLignes(serveurs, libellesZone)
	for i := range lignes {
		lignes[i].Anomalies = libellesAnomaliesDe(anomalies[lignes[i].ID])
	}
	s.rendreFragment(w, r, "serveurs_tableau", map[string]any{
		"Serveurs": lignes,
		"Erreur":   erreur,
		"Export":   exportServeurs(f),
	})
}

// libellesAnomaliesDe rend les libellés d'affichage d'une liste d'anomalies.
func libellesAnomaliesDe(anomalies []depot.AnomalieReseau) []string {
	var out []string
	for _, a := range anomalies {
		out = append(out, libelleAnomalie(a.Type))
	}
	return out
}

func (s *serveur) serveursTableau(w http.ResponseWriter, r *http.Request) {
	f, err := serveurFiltreDepuisRequete(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.rendreServeursTableau(w, r, f, "")
}

// serveursCreer : formulaire minimal (physical_name, hostname, zone,
// statut, commentaire). Les autres attributs documentaires se saisissent
// depuis la page de détail une fois le serveur créé.
//
// Depuis la v2.1, un scénario facultatif (champ "scenario_id") permet de
// créer un serveur HYPOTHESE : CreerServeur applique lui-même l'invariant 5
// (HYPOTHESE exige un scénario), traduit en erreur métier affichée sur la
// ligne si l'utilisateur choisit HYPOTHESE sans scénario ou l'inverse.
func (s *serveur) serveursCreer(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "formulaire illisible", http.StatusBadRequest)
		return
	}
	dc, err := idOptionnel(r, "zone_id")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	scenarioID, err := idOptionnel(r, "scenario_id")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	_, errCreation := s.depotPour(r).CreerServeur(depot.Serveur{
		PhysicalName: texteOptionnel(r, "physical_name"),
		Hostname:     texteOptionnel(r, "hostname"),
		ZoneID:       dc,
		Statut:       r.FormValue("statut"),
		ScenarioID:   scenarioID,
		Commentaire:  texteOptionnel(r, "commentaire"),
	})
	if errCreation != nil && !erreurMetier(errCreation) {
		s.erreurServeur(w, r, errCreation)
		return
	}

	f, err := serveurFiltreDepuisRequete(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if scenarioID != nil {
		// sans ça, un serveur HYPOTHESE qu'on vient de créer disparaîtrait
		// aussitôt du tableau réaffiché : le filtre réel (par défaut) l'exclut.
		f.ScenarioID = scenarioID
	}
	s.rendreServeursTableau(w, r, f, messageUtilisateur(errCreation))
}

// --------------------------------------------------------- détail (v1.3)

// rattachementLigne enrichit depot.Rattachement du code du modèle et du
// libellé de la révision : le gabarit ne doit pas avoir à résoudre lui-même
// revision_id -> modele_id -> code, ce serait de la logique métier dans le
// template.
type rattachementLigne struct {
	depot.Rattachement
	RevisionNumero  int64
	RevisionLibelle *string
	ModeleCode      string
}

// detailServeur est le contexte complet de la page /serveurs/{id} et de
// chacune de ses trois sections mutables (attributs, révision, affectation) :
// chaque section est un gabarit nommé séparé qui ne lit que le sous-ensemble
// de champs qui le concerne, ce qui permet de renvoyer, après une écriture
// dans une section, un fragment ciblé sans recharger toute la page.
type detailServeur struct {
	Serveur         depot.Serveur
	Zones           []depot.Zone
	Modeles         []depot.Modele
	Clusters        []depot.Cluster
	StatutsCreation []string

	RevisionCourante *rattachementLigne // nil si aucune période ouverte
	Rattachements    []rattachementLigne

	// AffectationActive est l'affectation effective à aujourd'hui : celle du
	// réel si ScenarioID est 0, sinon celle résolue sous ce scénario (voir
	// depot.AffectationEffectiveServeur) — nil si aucune.
	AffectationActive *depot.VieServeur
	Historique        []depot.VieServeur

	Scenarios  []depot.Scenario
	ScenarioID int64 // 0 = réel, même convention que TierIDOuZero

	// Réseau (v3.1) : typologie en énumération, VLAN forçable dans les
	// attributs, VLAN applicables à l'affectation active pour l'attribution
	// d'une adresse, anomalies calculées à la lecture.
	Typologies       []string
	Vlans            []depot.Vlan
	VlanIDOuZero     int64
	VlansApplicables []depot.Vlan
	Anomalies        []depot.AnomalieReseau
	ErreurAdressage  string
	CSRFToken        string // formulaire classique d'attribution d'adresse

	// Liens d'export (v3.0) des deux tableaux de la page, nommés
	// « rattachements » et « affectations » — le second suit le scénario
	// affiché. Vers /serveurs/{id}, dont le handler répond.
	ExportRattachements exportLiens
	ExportAffectations  exportLiens

	ErreurAttributs   string
	ErreurRevision    string
	ErreurAffectation string
}

// exportServeurDetail construit les liens d'export d'un tableau de la page de
// détail — sans requête sous la main, les sections étant aussi rendues en
// réponse à des POST (voir liensExportDepuis, tableau.go).
func exportServeurDetail(id int64, scenarioID *int64, nom string) exportLiens {
	q := url.Values{}
	if scenarioID != nil {
		q.Set("scenario", strconv.FormatInt(*scenarioID, 10))
	}
	return liensExportDepuis(fmt.Sprintf("/serveurs/%d", id), q, nom)
}

// tableauRattachements est la forme exportable de l'historique des
// rattachements de révision.
func tableauRattachements(srv depot.Serveur, lignes []rattachementLigne) tableau {
	t := tableau{Titre: "Rattachements — serveur " + strconv.FormatInt(srv.ID, 10), Colonnes: []string{
		"Modèle", "Révision", "Libellé", "Début", "Fin",
	}}
	for _, l := range lignes {
		t.Lignes = append(t.Lignes, []any{l.ModeleCode, l.RevisionNumero, texteOuVide(l.RevisionLibelle), l.DateDebut, texteOuVide(l.DateFin)})
	}
	return t
}

// tableauAffectations est la forme exportable de l'historique complet des
// affectations, avec le seau (réel ou scénario) de chaque vie.
func tableauAffectations(srv depot.Serveur, vies []depot.VieServeur) tableau {
	t := tableau{Titre: "Affectations — serveur " + strconv.FormatInt(srv.ID, 10), Colonnes: []string{
		"Cluster", "Début", "Fin", "Seau",
	}}
	for _, v := range vies {
		seau := "réel"
		if v.ScenarioID != nil {
			seau = "scénario"
		}
		t.Lignes = append(t.Lignes, []any{v.ClusterNom, v.DateDebut, texteOuVide(v.DateFin), seau})
	}
	return t
}

// ZoneIDOuZero — même raison que clusterLigne.TierIDOuZero : comparer
// un *int64 à l'id d'une <option> dans le gabarit exige un int64 nu.
func (d detailServeur) ZoneIDOuZero() int64 {
	if d.Serveur.ZoneID == nil {
		return 0
	}
	return *d.Serveur.ZoneID
}

func (s *serveur) enrichirRattachements(rs []depot.Rattachement) ([]rattachementLigne, error) {
	out := make([]rattachementLigne, len(rs))
	codesModele := map[int64]string{}
	for i, rt := range rs {
		rev, err := s.depot.LireRevision(rt.RevisionID)
		if err != nil {
			return nil, err
		}
		code, ok := codesModele[rev.ModeleID]
		if !ok {
			m, err := s.depot.LireModele(rev.ModeleID)
			if err != nil {
				return nil, err
			}
			code = m.Code
			codesModele[rev.ModeleID] = code
		}
		out[i] = rattachementLigne{
			Rattachement: rt, RevisionNumero: rev.Numero,
			RevisionLibelle: rev.Libelle, ModeleCode: code,
		}
	}
	return out, nil
}

// construireDetailServeur recharge l'intégralité du contexte de la page de
// détail à partir de la base. Appelé après chaque écriture (succès ou échec
// métier) : c'est ce qui garantit que le fragment renvoyé reflète l'état réel
// plutôt qu'une reconstruction partielle depuis le formulaire (règle n°5).
//
// scenarioID ne joue que sur l'affectation affichée (AffectationActive et
// Historique) : attributs et révision restent toujours ceux du réel, une
// révision ne se surcharge pas par scénario.
func (s *serveur) construireDetailServeur(id int64, scenarioID *int64) (detailServeur, error) {
	srv, err := s.depot.LireServeur(id)
	if err != nil {
		return detailServeur{}, err
	}
	// Un serveur hypothétique n'existe que dans son scénario : ouvert sans
	// scénario demandé, la section affectation se place d'elle-même sur ce
	// scénario plutôt que d'afficher, à tort, « aucune affectation active ».
	if scenarioID == nil && srv.ScenarioID != nil {
		scenarioID = srv.ScenarioID
	}
	dcs, err := s.depot.ListerZones()
	if err != nil {
		return detailServeur{}, err
	}
	modeles, err := s.depot.ListerModeles(false)
	if err != nil {
		return detailServeur{}, err
	}
	clusters, err := s.depot.ListerClusters(depot.FiltreCluster{})
	if err != nil {
		return detailServeur{}, err
	}
	scenarios, err := s.depot.ListerScenarios(false)
	if err != nil {
		return detailServeur{}, err
	}

	rattachements, err := s.depot.ListerRattachements(id)
	if err != nil {
		return detailServeur{}, err
	}
	lignesRatt, err := s.enrichirRattachements(rattachements)
	if err != nil {
		return detailServeur{}, err
	}
	var courante *rattachementLigne
	for i := range lignesRatt {
		if lignesRatt[i].DateFin == nil {
			courante = &lignesRatt[i]
			break
		}
	}

	historique, err := s.depot.HistoriqueServeur(id, scenarioID)
	if err != nil {
		return detailServeur{}, err
	}

	var active *depot.VieServeur
	if scenarioID == nil {
		for i := range historique {
			if historique[i].DateFin == nil && historique[i].ScenarioID == nil {
				active = &historique[i]
				break
			}
		}
	} else {
		eff, err := s.depot.AffectationEffectiveServeur(id, scenarioID, time.Now().Format("2006-01-02"))
		if err != nil {
			return detailServeur{}, err
		}
		if eff != nil {
			c, err := s.depot.LireCluster(eff.ClusterID)
			if err != nil {
				return detailServeur{}, err
			}
			active = &depot.VieServeur{Affectation: *eff, ClusterNom: c.Nom, ClusterProjetID: c.ProjetID}
		}
	}

	var scenarioIDOuZero int64
	if scenarioID != nil {
		scenarioIDOuZero = *scenarioID
	}

	vlans, err := s.depot.ListerVlans()
	if err != nil {
		return detailServeur{}, err
	}
	var vlanIDOuZero int64
	if srv.VlanID != nil {
		vlanIDOuZero = *srv.VlanID
	}
	var applicables []depot.Vlan
	if active != nil {
		c, err := s.depot.LireCluster(active.Affectation.ClusterID)
		if err != nil {
			return detailServeur{}, err
		}
		applicables, err = s.depot.VlansApplicables(c.ProjetID, c.EnvironnementID, srv.ZoneID, c.ID)
		if err != nil {
			return detailServeur{}, err
		}
	}
	parServeur, err := s.anomaliesParServeur()
	if err != nil {
		return detailServeur{}, err
	}

	return detailServeur{
		Serveur: srv, Zones: dcs, Modeles: modeles, Clusters: clusters,
		StatutsCreation:     statutsCreation,
		Typologies:          depot.Typologies,
		Vlans:               vlans,
		VlanIDOuZero:        vlanIDOuZero,
		VlansApplicables:    applicables,
		Anomalies:           parServeur[id],
		RevisionCourante:    courante,
		Rattachements:       lignesRatt,
		AffectationActive:   active,
		Historique:          historique,
		Scenarios:           scenarios,
		ScenarioID:          scenarioIDOuZero,
		ExportRattachements: exportServeurDetail(id, nil, "rattachements"),
		ExportAffectations:  exportServeurDetail(id, scenarioID, "affectations"),
	}, nil
}

func (s *serveur) serveurDetailPage(w http.ResponseWriter, r *http.Request) {
	id, err := idChemin(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	donnees, err := s.construireDetailServeur(id, scenarioDepuisRequete(r))
	if err != nil {
		if errors.Is(err, depot.ErrIntrouvable) {
			http.Error(w, "serveur introuvable", http.StatusNotFound)
			return
		}
		s.erreurServeur(w, r, err)
		return
	}
	// export universel (tableau.go), deux tableaux nommés : l'historique des
	// rattachements (toujours réel) et celui des affectations (réel plus
	// surcharges du scénario affiché, comme la table).
	if s.exporterNomme(w, r, "rattachements", func() (tableau, error) {
		return tableauRattachements(donnees.Serveur, donnees.Rattachements), nil
	}) {
		return
	}
	if s.exporterNomme(w, r, "affectations", func() (tableau, error) {
		return tableauAffectations(donnees.Serveur, donnees.Historique), nil
	}) {
		return
	}
	// retour de POST /adressage/serveurs/{id} : l'erreur métier voyage dans
	// l'URL ; le formulaire d'attribution est un <form> classique, jeton
	// CSRF en champ caché.
	donnees.ErreurAdressage = strings.TrimSpace(r.URL.Query().Get("erreur_adressage"))
	if session, ok := sessionDepuis(r.Context()); ok {
		donnees.CSRFToken = session.CSRFToken
	}
	s.rendrePage(w, r, s.titre(r, "titre.serveur"), "serveur_detail_page", donnees)
}

// serveurRevisionsDuModele alimente le second <select> du formulaire de
// rattachement (cascade modèle -> révision) : appelé par htmx au changement
// du <select> modèle, il renvoie un <select name="revision_id"> complet qui
// remplace le précédent (outerHTML).
func (s *serveur) serveurRevisionsDuModele(w http.ResponseWriter, r *http.Request) {
	modeleID := idRequis(r, "modele_id")
	revisions, err := s.depot.ListerRevisions(modeleID)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	s.rendreFragment(w, r, "serveur_select_revisions", revisions)
}

// serveurAttributsModifier repart toujours de l'enregistrement en base
// (actuel) et n'y superpose que les champs portés par le formulaire : les
// colonnes qu'aucun écran ne permet encore d'éditer (vlan_id — le catalogue
// VLAN est v3.1) ne sont donc jamais écrasées, y compris en cas de succès —
// pas seulement au réaffichage d'une erreur (règle n°5 généralisée).
func (s *serveur) serveurAttributsModifier(w http.ResponseWriter, r *http.Request) {
	id, err := idChemin(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "formulaire illisible", http.StatusBadRequest)
		return
	}
	actuel, err := s.depot.LireServeur(id)
	if err != nil {
		s.repondreDetailIntrouvable(w, r, err)
		return
	}
	dc, err := idOptionnel(r, "zone_id")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	vlanID, err := idOptionnel(r, "vlan_id")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	maj := actuel
	maj.VlanID = vlanID // forcer un VLAN (v3.1) passe par le formulaire des attributs, comme l'IP
	maj.PhysicalName = texteOptionnel(r, "physical_name")
	maj.Hostname = texteOptionnel(r, "hostname")
	maj.SerialNumber = texteOptionnel(r, "serial_number")
	maj.ZoneID = dc
	maj.PositionZone = texteOptionnel(r, "position_zone")
	maj.IP = texteOptionnel(r, "ip")
	maj.VersionOS = texteOptionnel(r, "version_os")
	maj.Typologie = texteOptionnel(r, "typologie")
	maj.CodeAppli = texteOptionnel(r, "code_appli")
	maj.DemandeRef = texteOptionnel(r, "demande_ref")
	maj.DemandeServeurRef = texteOptionnel(r, "demande_serveur_ref")
	maj.Commentaire = texteOptionnel(r, "commentaire")

	errModif := s.depotPour(r).ModifierServeur(maj)
	donnees, errC := s.construireDetailServeur(id, scenarioDepuisRequete(r))
	if errC != nil {
		s.erreurServeur(w, r, errC)
		return
	}
	if errModif != nil {
		if !erreurMetier(errModif) {
			s.erreurServeur(w, r, errModif)
			return
		}
		donnees.Serveur = maj // l'utilisateur ne perd pas sa saisie
		donnees.ErreurAttributs = messageUtilisateur(errModif)
	}
	s.rendreFragment(w, r, "serveur_section_attributs", donnees)
}

func (s *serveur) serveurStatutChanger(w http.ResponseWriter, r *http.Request) {
	id, err := idChemin(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "formulaire illisible", http.StatusBadRequest)
		return
	}
	errChangement := s.depotPour(r).ChangerStatut(id, r.FormValue("statut"))
	donnees, errC := s.construireDetailServeur(id, scenarioDepuisRequete(r))
	if errC != nil {
		if errors.Is(errC, depot.ErrIntrouvable) {
			s.repondreDetailIntrouvable(w, r, errC)
			return
		}
		s.erreurServeur(w, r, errC)
		return
	}
	if errChangement != nil {
		if !erreurMetier(errChangement) {
			s.erreurServeur(w, r, errChangement)
			return
		}
		donnees.ErreurAttributs = messageUtilisateur(errChangement)
		s.rendreFragment(w, r, "serveur_section_attributs", donnees)
		return
	}
	// un changement de statut peut clore l'affectation et le rattachement
	// (décommissionnement) : la fiche entière est rechargée pour que les
	// sections concernées reflètent l'état réel.
	w.Header().Set("HX-Redirect", fmt.Sprintf("/serveurs/%d", id))
	w.WriteHeader(http.StatusOK)
}

func (s *serveur) serveurRevisionRattacher(w http.ResponseWriter, r *http.Request) {
	id, err := idChemin(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "formulaire illisible", http.StatusBadRequest)
		return
	}
	revisionID := idRequis(r, "revision_id")
	errRattachement := s.depotPour(r).RattacherRevision(id, revisionID, r.FormValue("date_debut"), texteOptionnel(r, "commentaire"))
	donnees, errC := s.construireDetailServeur(id, scenarioDepuisRequete(r))
	if errC != nil {
		s.erreurServeur(w, r, errC)
		return
	}
	if errRattachement != nil {
		if !erreurMetier(errRattachement) {
			s.erreurServeur(w, r, errRattachement)
			return
		}
		donnees.ErreurRevision = messageUtilisateur(errRattachement)
	}
	s.rendreFragment(w, r, "serveur_section_revision", donnees)
}

// serveurAffectationSection recharge seulement la section affectation, pour
// le sélecteur de scénario de cette section (changer de scénario ne doit pas
// recharger toute la page, ni perdre une saisie en cours dans les autres
// sections).
func (s *serveur) serveurAffectationSection(w http.ResponseWriter, r *http.Request) {
	id, err := idChemin(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	donnees, err := s.construireDetailServeur(id, scenarioDepuisRequete(r))
	if err != nil {
		s.repondreDetailIntrouvable(w, r, err)
		return
	}
	s.rendreFragment(w, r, "serveur_section_affectation", donnees)
}

// serveurAffecter et serveurReaffecter appellent toutes deux
// depot.AffecterDansScenario sous un scénario : la distinction entre
// « première affectation » et « déplacement » ne compte que dans le seau
// visé (vierge ou pas), pas dans l'écran — voir le commentaire de tête de
// routesServeurs.
func (s *serveur) serveurAffecter(w http.ResponseWriter, r *http.Request) {
	id, err := idChemin(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "formulaire illisible", http.StatusBadRequest)
		return
	}
	clusterID := idRequis(r, "cluster_id")
	dateDebut, commentaire := r.FormValue("date_debut"), texteOptionnel(r, "commentaire")

	var errAffect error
	if scenarioID := scenarioDepuisRequete(r); scenarioID != nil {
		errAffect = s.depotPour(r).AffecterDansScenario(id, clusterID, dateDebut, *scenarioID, commentaire)
	} else {
		errAffect = s.depotPour(r).Affecter(id, clusterID, dateDebut, nil, commentaire)
	}
	s.repondreAffectation(w, r, id, errAffect)
}

func (s *serveur) serveurReaffecter(w http.ResponseWriter, r *http.Request) {
	id, err := idChemin(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "formulaire illisible", http.StatusBadRequest)
		return
	}
	clusterID := idRequis(r, "cluster_id")
	dateDebut, commentaire := r.FormValue("date_debut"), texteOptionnel(r, "commentaire")

	var errReaffect error
	if scenarioID := scenarioDepuisRequete(r); scenarioID != nil {
		errReaffect = s.depotPour(r).AffecterDansScenario(id, clusterID, dateDebut, *scenarioID, commentaire)
	} else {
		errReaffect = s.depotPour(r).Reaffecter(id, clusterID, dateDebut, nil, commentaire)
	}
	s.repondreAffectation(w, r, id, errReaffect)
}

// serveurDesaffecter retrouve lui-même l'affectation active à clôturer : le
// formulaire de désaffectation, sur la page de détail, ne connaît que le
// serveur, pas l'identifiant de l'affectation. Sous un scénario, c'est
// depot.RetirerDuScenario (voir modele-donnees.md §6) plutôt que Desaffecter,
// qui n'agit que dans le réel.
func (s *serveur) serveurDesaffecter(w http.ResponseWriter, r *http.Request) {
	id, err := idChemin(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "formulaire illisible", http.StatusBadRequest)
		return
	}
	dateFin := r.FormValue("date_fin")

	if scenarioID := scenarioDepuisRequete(r); scenarioID != nil {
		errRetrait := s.depotPour(r).RetirerDuScenario(id, *scenarioID, dateFin)
		s.repondreAffectation(w, r, id, errRetrait)
		return
	}

	actives, err := s.depot.ListerAffectationsServeur(id)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	var activeID int64
	var trouve bool
	for _, a := range actives {
		if a.DateFin == nil && a.ScenarioID == nil {
			activeID, trouve = a.ID, true
			break
		}
	}

	var errDesaffect error
	if !trouve {
		errDesaffect = fmt.Errorf("désaffectation du serveur %d : %w : aucune affectation active à clôturer", id, depot.ErrIntrouvable)
	} else {
		errDesaffect = s.depotPour(r).Desaffecter(activeID, dateFin)
	}
	s.repondreAffectation(w, r, id, errDesaffect)
}

func (s *serveur) repondreAffectation(w http.ResponseWriter, r *http.Request, id int64, errMutation error) {
	donnees, errC := s.construireDetailServeur(id, scenarioDepuisRequete(r))
	if errC != nil {
		s.erreurServeur(w, r, errC)
		return
	}
	if errMutation != nil {
		if !erreurMetier(errMutation) {
			s.erreurServeur(w, r, errMutation)
			return
		}
		donnees.ErreurAffectation = messageUtilisateur(errMutation)
	}
	s.rendreFragment(w, r, "serveur_section_affectation", donnees)
}

// repondreDetailIntrouvable est l'équivalent de repondreIntrouvable pour les
// fragments de la page de détail : même geste (200, message inline), mais
// sans dépendre d'un <td colspan> puisque ces fragments ne sont pas des
// lignes de tableau.
func (s *serveur) repondreDetailIntrouvable(w http.ResponseWriter, r *http.Request, err error) {
	if !errors.Is(err, depot.ErrIntrouvable) {
		s.erreurServeur(w, r, err)
		return
	}
	pasDeCache(w)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(`<p class="message-erreur">Serveur introuvable — recharger la page.</p>`))
}

// ------------------------------------------------- sans affectation active

// serveurSansAffectationLigne porte le formulaire d'affectation rapide
// (cluster + date) et l'éventuel message d'erreur, en plus de la ligne
// dépôt (serveur + fin de lease).
type serveurSansAffectationLigne struct {
	depot.ServeurSansAffectation
	ZoneLibelle string
	Clusters    []depot.Cluster
	Erreur      string
	ScenarioID  int64 // scénario affiché, porté par le champ caché de la ligne (0 = réel)
}

// serveursSansAffectationPage est paramétrable par scénario (v2.1) : sous un
// scénario, un serveur retiré par ce scénario redevient candidat à la
// réutilisation, et l'action rapide affecte dans le seau du scénario.
func (s *serveur) serveursSansAffectationPage(w http.ResponseWriter, r *http.Request) {
	scenarioID := scenarioDepuisRequete(r)
	items, err := s.depot.ListerServeursSansAffectationActive(scenarioID)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	libellesZone, err := s.chargerLibellesZone()
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	// export universel (tableau.go) : mêmes candidats, même scénario que la
	// page ; le serveur est désigné comme à l'écran (nom physique, à défaut
	// son numéro).
	if s.exporter(w, r, func() (tableau, error) {
		t := tableau{Titre: "Serveurs sans affectation", Colonnes: []string{"Serveur", "Hostname", "Zone", "Fin de lease"}}
		for _, it := range items {
			nom := "#" + strconv.FormatInt(it.Serveur.ID, 10)
			if it.Serveur.PhysicalName != nil {
				nom = *it.Serveur.PhysicalName
			}
			zone := ""
			if it.Serveur.ZoneID != nil {
				zone = libellesZone[*it.Serveur.ZoneID]
			}
			t.Lignes = append(t.Lignes, []any{nom, texteOuVide(it.Serveur.Hostname), zone, texteOuVide(it.FinLease)})
		}
		return t, nil
	}) {
		return
	}
	clusters, err := s.depot.ListerClusters(depot.FiltreCluster{})
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	selecteur, err := s.chargerSelecteurScenarios(r)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	s.rendrePage(w, r, s.titre(r, "titre.serveurs_sans_affectation"), "serveurs_sans_affectation_page", map[string]any{
		"Lignes":     serveursSansAffectationEnLignes(items, clusters, libellesZone, selecteur.ScenarioActifID),
		"Scenarios":  selecteur.Scenarios,
		"ScenarioID": selecteur.ScenarioActifID,
		"Export":     liensExport(r),
	})
}

func serveursSansAffectationEnLignes(items []depot.ServeurSansAffectation, clusters []depot.Cluster, libellesZone map[int64]string, scenarioID int64) []serveurSansAffectationLigne {
	out := make([]serveurSansAffectationLigne, len(items))
	for i, it := range items {
		l := serveurSansAffectationLigne{ServeurSansAffectation: it, Clusters: clusters, ScenarioID: scenarioID}
		if it.Serveur.ZoneID != nil {
			l.ZoneLibelle = libellesZone[*it.Serveur.ZoneID]
		}
		out[i] = l
	}
	return out
}

// serveursSansAffectationAffecter : voir le commentaire de tête de
// serveurs/sans_affectation.html pour la décision de conception (succès ->
// la ligne disparaît, la réponse est vide ; échec -> la ligne se réaffiche
// avec un message, la ressaisie reste possible).
func (s *serveur) serveursSansAffectationAffecter(w http.ResponseWriter, r *http.Request) {
	id, err := idChemin(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "formulaire illisible", http.StatusBadRequest)
		return
	}
	clusterID := idRequis(r, "cluster_id")
	scenarioID := scenarioDepuisRequete(r)
	var errAffect error
	if scenarioID != nil {
		errAffect = s.depotPour(r).AffecterDansScenario(id, clusterID, r.FormValue("date_debut"), *scenarioID, nil)
	} else {
		errAffect = s.depotPour(r).Affecter(id, clusterID, r.FormValue("date_debut"), nil, nil)
	}
	if errAffect == nil {
		pasDeCache(w)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		// réponse vide : la ligne n'a plus sa place dans cette liste, htmx la
		// remplace par rien (hx-swap="outerHTML" sur la ligne).
		return
	}
	if !erreurMetier(errAffect) {
		s.erreurServeur(w, r, errAffect)
		return
	}

	items, errListe := s.depot.ListerServeursSansAffectationActive(scenarioID)
	if errListe != nil {
		s.erreurServeur(w, r, errListe)
		return
	}
	var item *depot.ServeurSansAffectation
	for i := range items {
		if items[i].Serveur.ID == id {
			item = &items[i]
			break
		}
	}
	if item == nil {
		// un autre utilisateur a affecté ce serveur entretemps (ErrChevauchement
		// concurrent) : il n'a plus sa place ici non plus, même geste que le succès.
		pasDeCache(w)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		return
	}

	clusters, errC := s.depot.ListerClusters(depot.FiltreCluster{})
	if errC != nil {
		s.erreurServeur(w, r, errC)
		return
	}
	libellesZone, errZone := s.chargerLibellesZone()
	if errZone != nil {
		s.erreurServeur(w, r, errZone)
		return
	}
	var scenarioIDOuZero int64
	if scenarioID != nil {
		scenarioIDOuZero = *scenarioID
	}
	ligne := serveurSansAffectationLigne{ServeurSansAffectation: *item, Clusters: clusters, Erreur: messageUtilisateur(errAffect), ScenarioID: scenarioIDOuZero}
	if item.Serveur.ZoneID != nil {
		ligne.ZoneLibelle = libellesZone[*item.Serveur.ZoneID]
	}
	s.rendreFragment(w, r, "serveurs_sans_affectation_ligne", ligne)
}
