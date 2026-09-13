package web

import (
	"net/http"
	"net/url"
	"strconv"

	"parallax/internal/depot"
)

// Écrans d'adressage IP (backlog v3.1).
//
// L'attribution est un geste explicite sur une hypothèse — « adresser les
// serveurs de cette hypothèse » — jamais un effet de la création d'un
// serveur. Le pool est global (voir internal/depot/adressage.go). Les
// anomalies se calculent à la lecture et ne bloquent rien.
//
// routesAdressage() n'est pas appelée depuis server.go : l'intégration
// ajoute cet appel (voir la doc de tête de middleware.go).
//
//	GET  /adressage/anomalies          page des anomalies réseau (lecteur)
//	GET  /adressage/scenarios/{id}     état d'adressage de l'hypothèse : ce
//	                                   que ferait l'attribution, sans écrire
//	                                   (simulation) — sert aussi d'export
//	POST /adressage/scenarios/{id}     attribue, rend la même page (Ecrit)
//	POST /adressage/serveurs/{id}      adresse un serveur dans le VLAN choisi
//	                                   (champ vlan_id), 303 vers /serveurs/{id}
//
// La page GET rend l'export universel possible : après le POST, les liens
// d'export visent l'état courant de l'hypothèse (serveurs déjà adressés
// compris), pas un résultat transitoire qu'il faudrait mettre en cache.
func (s *serveur) routesAdressage() {
	s.mux.HandleFunc("GET /adressage/anomalies", s.lecteur(s.adressageAnomaliesPage))
	s.mux.HandleFunc("GET /adressage/scenarios/{id}", s.lecteur(s.adressageScenarioApercu))
	s.mux.HandleFunc("POST /adressage/scenarios/{id}", s.editeur(s.adressageScenarioAttribuer))
	s.mux.HandleFunc("POST /adressage/serveurs/{id}", s.editeur(s.adressageServeurAttribuer))
}

// anomaliesParServeur regroupe les anomalies réseau par serveur, pour poser
// un repère sur la fiche et la ligne d'un serveur (fourni à l'intégrateur
// des écrans serveurs ; jamais bloquant, l'erreur ne remonte que si la base
// ne répond pas).
func (s *serveur) anomaliesParServeur() (map[int64][]depot.AnomalieReseau, error) {
	anomalies, err := s.depot.AnomaliesReseau()
	if err != nil {
		return nil, err
	}
	out := map[int64][]depot.AnomalieReseau{}
	for _, a := range anomalies {
		out[a.ServeurID] = append(out[a.ServeurID], a)
	}
	return out, nil
}

// ---------------------------------------------------------------- libellés

var libellesAnomalies = map[string]string{
	depot.AnomalieAdresseMultiple:  "adresse multiple",
	depot.AnomalieHorsPlage:        "hors plage",
	depot.AnomalieVlanIncompatible: "VLAN incompatible",
	depot.AnomalieSansVlan:         "sans VLAN",
	depot.AnomalieIPInvalide:       "adresse invalide",
}

// ordreAnomalies fixe l'ordre des compteurs en tête de page.
var ordreAnomalies = []string{
	depot.AnomalieAdresseMultiple, depot.AnomalieHorsPlage, depot.AnomalieVlanIncompatible,
	depot.AnomalieSansVlan, depot.AnomalieIPInvalide,
}

var libellesIssuesAdressage = map[string]string{
	depot.AdressageAttribue:     "attribuée",
	depot.AdressageDejaAdresse:  "déjà adressé",
	depot.AdressageAChoisir:     "à choisir",
	depot.AdressageSansVlan:     "aucun VLAN applicable",
	depot.AdressagePlageEpuisee: "plage épuisée",
}

var ordreIssuesAdressage = []string{
	depot.AdressageAttribue, depot.AdressageAChoisir, depot.AdressageSansVlan,
	depot.AdressagePlageEpuisee, depot.AdressageDejaAdresse,
}

// libelleAnomalie et libelleIssue renvoient le libellé d'un type, ou le
// code brut si inconnu — jamais une chaîne vide dans un écran.
func libelleAnomalie(t string) string {
	if l, ok := libellesAnomalies[t]; ok {
		return l
	}
	return t
}

func libelleIssue(t string) string {
	if l, ok := libellesIssuesAdressage[t]; ok {
		return l
	}
	return t
}

// compteType est une case du bandeau de compteurs.
type compteType struct {
	Type    string
	Libelle string
	Nb      int
}

// ---------------------------------------------------------------- anomalies

type ligneAnomalie struct {
	depot.AnomalieReseau
	Libelle string
}

func (s *serveur) adressageAnomaliesPage(w http.ResponseWriter, r *http.Request) {
	anomalies, err := s.depot.AnomaliesReseau()
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	if s.exporter(w, r, func() (tableau, error) {
		t := tableau{Titre: "Anomalies réseau", Colonnes: []string{"Serveur", "Adresse", "VLAN", "Type", "Détail"}}
		for _, a := range anomalies {
			t.Lignes = append(t.Lignes, []any{a.PhysicalName, a.IP, a.VlanCode, libelleAnomalie(a.Type), a.Detail})
		}
		return t, nil
	}) {
		return
	}

	comptes := map[string]int{}
	lignes := make([]ligneAnomalie, len(anomalies))
	for i, a := range anomalies {
		comptes[a.Type]++
		lignes[i] = ligneAnomalie{AnomalieReseau: a, Libelle: libelleAnomalie(a.Type)}
	}
	var bandeau []compteType
	for _, t := range ordreAnomalies {
		bandeau = append(bandeau, compteType{Type: t, Libelle: libelleAnomalie(t), Nb: comptes[t]})
	}
	s.rendrePage(w, r, s.titre(r, "titre.adressage_anomalies"), "adressage_anomalies_page", map[string]any{
		"Anomalies": lignes, "Comptes": bandeau, "Total": len(anomalies),
		"Export": liensExport(r),
	})
}

// ---------------------------------------------------------------- scénario

type ligneResultatAdressage struct {
	depot.LigneAdressage
	Libelle string
}

// rendreResultatAdressage construit et rend la page de résultat, commune à
// l'aperçu (GET, ecrit=false) et à l'attribution (POST, ecrit=true).
func (s *serveur) rendreResultatAdressage(w http.ResponseWriter, r *http.Request, sc depot.Scenario, res depot.ResultatAdressage, ecrit bool) {
	chemin := "/adressage/scenarios/" + strconv.FormatInt(sc.ID, 10)
	if s.exporter(w, r, func() (tableau, error) {
		t := tableau{Titre: "Adressage " + sc.Nom, Colonnes: []string{"Serveur", "Cluster", "Issue", "Adresse", "VLAN", "Détail"}}
		for _, l := range res.Lignes {
			t.Lignes = append(t.Lignes, []any{l.PhysicalName, l.ClusterNom, libelleIssue(l.Issue), l.IP, l.VlanCode, l.Detail})
		}
		return t, nil
	}) {
		return
	}
	lignes := make([]ligneResultatAdressage, len(res.Lignes))
	for i, l := range res.Lignes {
		lignes[i] = ligneResultatAdressage{LigneAdressage: l, Libelle: libelleIssue(l.Issue)}
	}
	var bandeau []compteType
	for _, t := range ordreIssuesAdressage {
		bandeau = append(bandeau, compteType{Type: t, Libelle: libelleIssue(t), Nb: res.Comptes[t]})
	}
	s.rendrePage(w, r, s.titre(r, "titre.adressage")+" — "+sc.Nom, "adressage_resultat_page", map[string]any{
		"Scenario": sc, "Lignes": lignes, "Comptes": bandeau, "Ecrit": ecrit,
		"Chemin": chemin, "CSRFToken": s.csrfDepuisSession(r),
		"Export": liensExportDepuis(chemin, nil, ""),
	})
}

func (s *serveur) adressageScenarioApercu(w http.ResponseWriter, r *http.Request) {
	s.adressageScenario(w, r, false)
}

func (s *serveur) adressageScenarioAttribuer(w http.ResponseWriter, r *http.Request) {
	s.adressageScenario(w, r, true)
}

func (s *serveur) adressageScenario(w http.ResponseWriter, r *http.Request, ecrire bool) {
	id, err := idChemin(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	sc, err := s.depot.LireScenario(id)
	if err != nil {
		if erreurMetier(err) {
			http.NotFound(w, r)
			return
		}
		s.erreurServeur(w, r, err)
		return
	}
	var res depot.ResultatAdressage
	if ecrire {
		res, err = s.depotPour(r).AdresserScenario(id)
	} else {
		res, err = s.depot.SimulerAdressage(id)
	}
	if err != nil {
		if erreurMetier(err) {
			// scénario abandonné : rien à adresser, on l'explique à la place
			// du tableau plutôt que de répondre une 4xx brute.
			s.rendrePage(w, r, s.titre(r, "titre.adressage")+" — "+sc.Nom, "adressage_resultat_page", map[string]any{
				"Scenario": sc, "Erreur": messageUtilisateur(err),
				"Chemin": "/adressage/scenarios/" + strconv.FormatInt(id, 10),
				"Export": liensExportDepuis("/adressage/scenarios/"+strconv.FormatInt(id, 10), nil, ""),
			})
			return
		}
		s.erreurServeur(w, r, err)
		return
	}
	s.rendreResultatAdressage(w, r, sc, res, ecrire)
}

// ---------------------------------------------------------------- serveur

// adressageServeurAttribuer adresse un serveur dans le VLAN choisi puis
// redirige vers sa fiche. Une erreur métier (déjà adressé, plage épuisée,
// VLAN inconnu) est transmise en paramètre erreur_adressage de la
// redirection : la fiche serveur peut l'afficher, ou l'ignorer.
func (s *serveur) adressageServeurAttribuer(w http.ResponseWriter, r *http.Request) {
	id, err := idChemin(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "formulaire illisible", http.StatusBadRequest)
		return
	}
	destination := "/serveurs/" + strconv.FormatInt(id, 10)
	if suite := r.FormValue("suite"); suite != "" && suite[0] == '/' {
		destination = suite // retour vers la page d'adressage du scénario, par exemple
	}
	_, err = s.depotPour(r).AdresserServeur(id, idRequis(r, "vlan_id"))
	if err != nil {
		if !erreurMetier(err) {
			s.erreurServeur(w, r, err)
			return
		}
		separateur := "?"
		if u, errURL := url.Parse(destination); errURL == nil && u.RawQuery != "" {
			separateur = "&"
		}
		destination += separateur + "erreur_adressage=" + url.QueryEscape(messageUtilisateur(err))
	}
	http.Redirect(w, r, destination, http.StatusSeeOther)
}
