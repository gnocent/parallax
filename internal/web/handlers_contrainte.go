package web

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"parallax/internal/capacity"
	"parallax/internal/depot"
)

// routesContraintes enregistre la section « Contraintes de dimensionnement »
// (v2.2 du backlog) d'un cluster. Décision de cadrage : elle vit sur l'écran
// besoin/offre (GET /clusters/{id}/besoin-offre, handlers_dimensionnement.go)
// sous forme d'un fragment chargé en htmx :
//
//	<div id="section-contraintes"
//	     hx-get="/clusters/{id}/contraintes?annee=…&scenario=…"
//	     hx-trigger="load" hx-swap="innerHTML"></div>
//
// Ce fichier ne câble pas cette div dans besoin_offre.html — c'est
// l'intégration qui l'ajoute (voir la tâche) — et ne modifie ni server.go ni
// aucun autre écran : il ne fournit que le fragment et ses trois routes.
//
// Convention de paramètres, alignée sur anneeDepuisRequete et
// scenarioDepuisRequete (handlers_dimensionnement.go, scenario_contexte.go) :
// anneeDepuisRequete ne lit QUE r.URL.Query() (jamais le corps d'un POST,
// contrairement à scenarioDepuisRequete qui passe par r.FormValue). Pour que
// les deux écritures (définir, supprimer) redemandent bien le même fragment
// après coup, année et scénario voyagent donc en paramètres de la CHAÎNE DE
// REQUÊTE des actions hx-post, jamais en simples champs cachés du corps —
// même patron que le bouton Supprimer de valeur_ligne
// (templates/variables/detail.html : « …/supprimer?scenario=… »).
func (s *serveur) routesContraintes() {
	s.mux.HandleFunc("GET /clusters/{id}/contraintes", s.lecteur(s.clusterContraintesSection))
	s.mux.HandleFunc("POST /clusters/{id}/contraintes", s.editeur(s.clusterContraintesDefinir))
	s.mux.HandleFunc("POST /clusters/{id}/contraintes/{contrainteID}/supprimer", s.editeur(s.clusterContraintesSupprimer))
}

// typeContrainteInfo associe un code de contrainte (internal/capacity) à sa
// clé d'aide i18n ("contrainte.aide.<code>"), affichée dans le <select> du
// formulaire « Définir » — le sens de chaque type tel qu'implémenté par
// capacity.AppliquerContraintes.
type typeContrainteInfo struct {
	Code string
}

// typesContrainteDisponibles fixe l'ordre d'affichage (dans le <select> et
// dans le bloc « effectif sous ce scénario ») : c'est aussi l'ordre dans
// lequel construireEffectives parcourt capacity.Contraintes, une map dont
// l'itération n'est pas déterministe — nécessaire pour un affichage stable.
var typesContrainteDisponibles = []typeContrainteInfo{
	{capacity.ContrainteMinTotal},
	{capacity.ContrainteMinParZone},
	{capacity.ContrainteMultipleTotal},
	{capacity.ContrainteMultipleParZone},
	{capacity.ContrainteNbZones},
	{capacity.ContrainteEquilibrageZone},
}

// contrainteLigne incorpore depot.Contrainte et ajoute un message d'erreur
// optionnel — même raison que projetLigne (handlers_projet.go) : un
// depot.Contrainte nu ne s'affiche jamais directement dans un gabarit.
type contrainteLigne struct {
	depot.Contrainte
	Erreur string
}

// EstDuScenario indique si cette ligne est une surcharge de scénario plutôt
// qu'une contrainte du réel — sert au badge, même principe que
// valeurLigne.EstDuScenario (handlers_variable.go).
func (l contrainteLigne) EstDuScenario() bool { return l.ScenarioID != nil }

// EstEquilibrage évite de répéter le code EQUILIBRAGE_ZONE en dur dans le
// gabarit : cette contrainte s'affiche par sa portée, les autres par leur
// valeur numérique.
func (l contrainteLigne) EstEquilibrage() bool { return l.Type == capacity.ContrainteEquilibrageZone }

// ValeurTexte expose Valeur (*float64) comme une chaîne pour le gabarit —
// html/template afficherait l'adresse mémoire d'un pointeur imprimé
// directement (même raison que DefautTexte, handlers_variable.go).
func (l contrainteLigne) ValeurTexte() string {
	if l.Valeur == nil {
		return ""
	}
	return strconv.FormatFloat(*l.Valeur, 'f', -1, 64)
}

// PorteeTexte expose Portee (*string) comme une chaîne pour le gabarit — même
// raison que ValeurTexte ; videSiNil ferait aussi l'affaire mais une méthode
// dédiée garde le gabarit lisible à côté de EstEquilibrage.
func (l contrainteLigne) PorteeTexte() string {
	if l.Portee == nil {
		return ""
	}
	return *l.Portee
}

func contraintesEnLignes(contraintes []depot.Contrainte) []contrainteLigne {
	out := make([]contrainteLigne, len(contraintes))
	for i, c := range contraintes {
		out[i] = contrainteLigne{Contrainte: c}
	}
	return out
}

// ligneEffective est une entrée du bloc « effectif sous ce scénario » :
// capacity.Contraintes (issu de depot.ContraintesResolues) réduit à ce que
// montre l'écran, dans l'ordre stable de typesContrainteDisponibles.
type ligneEffective struct {
	Type   string
	valeur float64
}

// ValeurTexte affiche « actif » pour EQUILIBRAGE_ZONE (un drapeau, pas une
// valeur numérique porteuse de sens — voir capacity.Contraintes) et la valeur
// numérique pour les autres types.
func (l ligneEffective) ValeurTexte() string {
	if l.Type == capacity.ContrainteEquilibrageZone {
		return "actif"
	}
	return strconv.FormatFloat(l.valeur, 'f', -1, 64)
}

// construireEffectives traduit capacity.Contraintes (une map, itération non
// déterministe) en tranche ordonnée pour l'affichage.
func construireEffectives(resolues capacity.Contraintes) []ligneEffective {
	var out []ligneEffective
	for _, def := range typesContrainteDisponibles {
		if v, ok := resolues[def.Code]; ok {
			out = append(out, ligneEffective{Type: def.Code, valeur: v})
		}
	}
	return out
}

// clusterContraintesSection est la cible de hx-get="/clusters/{id}/contraintes"
// (hx-trigger="load") : le fragment complet de la section, sans mise en page —
// exactement ce que rend aussi toute écriture, pour que la section affichée
// ne se distingue jamais d'un rechargement.
func (s *serveur) clusterContraintesSection(w http.ResponseWriter, r *http.Request) {
	clusterID, err := idChemin(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	annee := anneeDepuisRequete(r)
	scenarioID := scenarioDepuisRequete(r)
	s.rendreContraintesSection(w, r, clusterID, annee, scenarioID, "")
}

// clusterContraintesDefinir traite le formulaire « Définir » : DefinirContrainte
// est un upsert par (cluster, année, type, seau), donc cette route sert aussi
// bien à créer qu'à remplacer une contrainte existante — pas de distinction
// nécessaire côté écran, comme valeursCreerOuModifier (handlers_variable.go).
func (s *serveur) clusterContraintesDefinir(w http.ResponseWriter, r *http.Request) {
	clusterID, err := idChemin(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "formulaire illisible", http.StatusBadRequest)
		return
	}
	annee := anneeDepuisRequete(r)
	scenarioID := scenarioDepuisRequete(r)

	valeur, errValeur := flottantOptionnelModele(r, "valeur")
	typ := r.FormValue("type")
	portee := texteOptionnel(r, "portee")
	commentaire := texteOptionnel(r, "commentaire")

	var errEcriture error
	if errValeur != nil {
		errEcriture = errValeur
	} else {
		_, errEcriture = s.depotPour(r).DefinirContrainte(clusterID, scenarioID, annee, typ, valeur, portee, commentaire)
	}
	if errEcriture != nil && !erreurMetier(errEcriture) {
		s.erreurServeur(w, r, errEcriture)
		return
	}
	s.rendreContraintesSection(w, r, clusterID, annee, scenarioID, messageUtilisateur(errEcriture))
}

// clusterContraintesSupprimer retire une contrainte puis réaffiche la section
// entière sous l'année et le scénario de la requête — règle 4 de la tâche :
// jamais une ligne seule, toujours la section rechargée depuis la base.
func (s *serveur) clusterContraintesSupprimer(w http.ResponseWriter, r *http.Request) {
	clusterID, errC := idChemin(r)
	contrainteID, errID := idCheminNomme(r, "contrainteID")
	if errC != nil || errID != nil {
		http.Error(w, "identifiant invalide dans l'adresse", http.StatusBadRequest)
		return
	}
	annee := anneeDepuisRequete(r)
	scenarioID := scenarioDepuisRequete(r)

	err := s.depotPour(r).SupprimerContrainte(contrainteID)
	if err != nil && !erreurMetier(err) {
		s.erreurServeur(w, r, err)
		return
	}
	s.rendreContraintesSection(w, r, clusterID, annee, scenarioID, messageUtilisateur(err))
}

// sectionContraintes est le contexte du gabarit "contraintes_section", et ce
// que l'export de la page besoin/offre relit (handlers_dimensionnement.go) :
// les deux tableaux de la section et leurs liens d'export.
type sectionContraintes struct {
	ClusterID  int64
	Annee      int
	ScenarioID int64 // 0 = réel
	Lignes     []contrainteLigne
	Effectives []ligneEffective // seulement sous un scénario
	Erreur     string
	Types      []typeContrainteInfo

	// Liens d'export (v3.0) : la section est un fragment de la page
	// besoin/offre, les liens visent donc cette page, avec l'année et le
	// scénario courants — c'est clusterBesoinOffre qui répond.
	Export           exportLiens // tableau « contraintes »
	ExportEffectives exportLiens // tableau « contraintes_effectives »
}

// exportContraintes construit les liens d'export d'un tableau de la section
// vers la page besoin/offre du cluster, année et scénario compris.
func exportContraintes(clusterID int64, annee int, scenarioID *int64, nom string) exportLiens {
	q := url.Values{"annee": {strconv.Itoa(annee)}}
	if scenarioID != nil {
		q.Set("scenario", strconv.FormatInt(*scenarioID, 10))
	}
	return liensExportDepuis(fmt.Sprintf("/clusters/%d/besoin-offre", clusterID), q, nom)
}

// chargerContraintesSection recharge les contraintes brutes (ListerContraintes)
// et, sous un scénario, la carte effective (ContraintesResolues) qui montre
// ce qui gagne entre le réel et sa surcharge.
func (s *serveur) chargerContraintesSection(clusterID int64, annee int, scenarioID *int64, erreur string) (sectionContraintes, error) {
	brutes, err := s.depot.ListerContraintes(clusterID, annee, scenarioID)
	if err != nil {
		return sectionContraintes{}, err
	}

	var effectives []ligneEffective
	if scenarioID != nil {
		resolues, err := s.depot.ContraintesResolues(clusterID, annee, scenarioID)
		if err != nil {
			return sectionContraintes{}, err
		}
		effectives = construireEffectives(resolues)
	}

	var scenarioIDOuZero int64
	if scenarioID != nil {
		scenarioIDOuZero = *scenarioID
	}

	return sectionContraintes{
		ClusterID:        clusterID,
		Annee:            annee,
		ScenarioID:       scenarioIDOuZero,
		Lignes:           contraintesEnLignes(brutes),
		Effectives:       effectives,
		Erreur:           erreur,
		Types:            typesContrainteDisponibles,
		Export:           exportContraintes(clusterID, annee, scenarioID, "contraintes"),
		ExportEffectives: exportContraintes(clusterID, annee, scenarioID, "contraintes_effectives"),
	}, nil
}

// tableauContraintes est la forme exportable des contraintes brutes : la
// valeur ou la portée selon le type, comme la colonne affichée, et le seau.
func tableauContraintes(sc sectionContraintes) tableau {
	t := tableau{Titre: fmt.Sprintf("Contraintes — cluster %d — %d", sc.ClusterID, sc.Annee), Colonnes: []string{
		"Type", "Valeur / portée", "Commentaire", "Seau",
	}}
	for _, l := range sc.Lignes {
		var valeur any
		if l.EstEquilibrage() {
			valeur = texteOuVide(l.Portee)
		} else {
			valeur = nombreOuVide(l.Valeur)
		}
		seau := "réel"
		if l.EstDuScenario() {
			seau = "scénario"
		}
		t.Lignes = append(t.Lignes, []any{l.Type, valeur, texteOuVide(l.Commentaire), seau})
	}
	return t
}

// tableauContraintesEffectives est la forme exportable du bloc « effectif
// sous ce scénario » — ce qui gagne, réel ou surcharge.
func tableauContraintesEffectives(sc sectionContraintes) tableau {
	t := tableau{Titre: fmt.Sprintf("Contraintes effectives — cluster %d — %d", sc.ClusterID, sc.Annee), Colonnes: []string{"Type", "Valeur"}}
	for _, e := range sc.Effectives {
		t.Lignes = append(t.Lignes, []any{e.Type, e.ValeurTexte()})
	}
	return t
}

// rendreContraintesSection recharge la section (chargerContraintesSection)
// puis rend le fragment complet.
func (s *serveur) rendreContraintesSection(w http.ResponseWriter, r *http.Request, clusterID int64, annee int, scenarioID *int64, erreur string) {
	section, err := s.chargerContraintesSection(clusterID, annee, scenarioID, erreur)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	s.rendreFragment(w, r, "contraintes_section", section)
}
