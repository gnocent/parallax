package web

import (
	"net/http"
	"sort"
	"strings"
	"time"

	"parallax/internal/depot"
)

// scenario_contexte.go porte ce qui est commun à tous les écrans
// paramétrables par scénario (v2.1 du backlog) : lire le scénario choisi
// depuis la requête, et construire la liste des scénarios proposables dans
// un sélecteur. Convention d'URL : le paramètre "scenario" (GET query ou
// champ de formulaire cohabitent via r.FormValue), absent ou "0" -> réel.
//
// Un écran paramétrable par scénario suit ce patron :
//
//  1. scenarioID, err := scenarioDepuisRequete(r)
//  2. transmettre scenarioID (au lieu d'un nil en dur) aux lectures du dépôt
//     qui l'acceptent déjà (AffectationsResolues, ResoudreValeur, DefinirValeur,
//     ContraintesResolues, vues.ChargerParcCourant…)
//  3. exposer aux gabarits, via chargerSelecteurScenarios, la liste pour le
//     <select> et l'identifiant actuellement choisi

// dateDepuisRequete lit le paramètre "date" (YYYY-MM-DD) des écrans qui se
// lisent à une date (vues, comparaison — backlog v3.0) : absent ou vide,
// c'est aujourd'hui ; une valeur mal formée est ignorée de la même façon,
// un sélecteur mal formé ne devant jamais faire tomber un écran de lecture.
func dateDepuisRequete(r *http.Request) string {
	if v := strings.TrimSpace(r.FormValue("date")); v != "" {
		if depot.ValiderDate("date", v) == nil {
			return v
		}
	}
	return time.Now().Format("2006-01-02")
}

// scenarioDepuisRequete lit le paramètre "scenario" : absent, vide ou "0" ->
// réel (nil). Une valeur non numérique est traitée comme absente plutôt que
// de faire échouer la requête — un sélecteur mal formé ne doit jamais faire
// tomber un écran de lecture.
func scenarioDepuisRequete(r *http.Request) *int64 {
	id, err := idOptionnel(r, "scenario")
	if err != nil || id == nil || *id == 0 {
		return nil
	}
	return id
}

// selecteurScenarios est le contexte à fusionner dans les données d'un
// gabarit qui propose un sélecteur de scénario.
type selecteurScenarios struct {
	Scenarios       []depot.Scenario
	ScenarioActifID int64 // 0 = réel, cohérent avec TierIDOuZero et consorts
}

// chargerSelecteurScenarios liste les scénarios proposables (BROUILLON et
// ACTIF — un scénario clos ne se choisit plus dans un sélecteur, il reste
// consultable depuis l'écran des scénarios lui-même) et retient lequel la
// requête a choisi.
func (s *serveur) chargerSelecteurScenarios(r *http.Request) (selecteurScenarios, error) {
	scenarios, err := s.depot.ListerScenarios(false)
	if err != nil {
		return selecteurScenarios{}, err
	}
	sort.Slice(scenarios, func(i, j int) bool { return scenarios[i].Nom < scenarios[j].Nom })

	sel := selecteurScenarios{Scenarios: scenarios}
	if id := scenarioDepuisRequete(r); id != nil {
		sel.ScenarioActifID = *id
	}
	return sel, nil
}
