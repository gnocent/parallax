package web

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"parallax/internal/depot"
)

// TestScenarioCreationDescriptionVideRefusee vérifie la règle n°2 du patron
// d'écran : une erreur métier (ici ErrValidation, description obligatoire)
// répond 200 avec le message inline, jamais un code 4xx/5xx, et ne crée rien.
func TestScenarioCreationDescriptionVideRefusee(t *testing.T) {
	serveur, d := serveurDeTest(t)
	client := clientConnecte(t, serveur)

	repPage, err := client.Get(serveur.URL + "/scenarios")
	if err != nil {
		t.Fatal(err)
	}
	jeton := csrfDepuisPage(t, corps(t, repPage))

	champs := url.Values{"nom": {"Sans description"}, "description": {""}}
	rep, err := client.Do(requeteFormulaire(t, http.MethodPost, serveur.URL+"/scenarios", jeton, champs))
	if err != nil {
		t.Fatal(err)
	}
	if rep.StatusCode != http.StatusOK {
		t.Fatalf("erreur métier : attendu 200, obtenu %d", rep.StatusCode)
	}
	frag := corps(t, rep)
	if !strings.Contains(frag, "obligatoire") {
		t.Fatalf("le message d'erreur métier devrait apparaître dans le fragment : %s", frag)
	}

	scenarios, err := d.ListerScenarios(true)
	if err != nil {
		t.Fatal(err)
	}
	if len(scenarios) != 0 {
		t.Fatalf("aucun scénario n'aurait dû être créé : %+v", scenarios)
	}
}

// TestScenarioCycleStatuts couvre brouillon -> actif -> abandonné -> repris,
// en vérifiant à chaque étape la ligne renvoyée par le fragment ET l'état
// persisté en base.
func TestScenarioCycleStatuts(t *testing.T) {
	serveur, d := serveurDeTest(t)
	client := clientConnecte(t, serveur)

	repPage, err := client.Get(serveur.URL + "/scenarios")
	if err != nil {
		t.Fatal(err)
	}
	jeton := csrfDepuisPage(t, corps(t, repPage))

	champs := url.Values{"nom": {"Bascule Kafka"}, "description": {"Hypothèse de test"}}
	repCreation, err := client.Do(requeteFormulaire(t, http.MethodPost, serveur.URL+"/scenarios", jeton, champs))
	if err != nil {
		t.Fatal(err)
	}
	if repCreation.StatusCode != http.StatusOK {
		t.Fatalf("création : attendu 200, obtenu %d", repCreation.StatusCode)
	}
	fragCreation := corps(t, repCreation)
	if !strings.Contains(fragCreation, "Bascule Kafka") || !strings.Contains(fragCreation, "brouillon") {
		t.Fatalf("le scénario créé devrait apparaître en brouillon : %s", fragCreation)
	}

	scenarios, err := d.ListerScenarios(false)
	if err != nil {
		t.Fatal(err)
	}
	if len(scenarios) != 1 {
		t.Fatalf("un scénario aurait dû être créé : %+v", scenarios)
	}
	id := scenarios[0].ID

	// activation
	repActiver, err := client.Do(requeteFormulaire(t, http.MethodPost,
		serveur.URL+"/scenarios/"+strconv.FormatInt(id, 10)+"/activer", jeton, nil))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(corps(t, repActiver), "actif") {
		t.Fatal("le scénario activé devrait porter le badge actif")
	}
	sc, err := d.LireScenario(id)
	if err != nil {
		t.Fatal(err)
	}
	if sc.Statut != depot.ScenarioActif {
		t.Fatalf("statut attendu ACTIF, obtenu %s", sc.Statut)
	}

	// abandon
	repAbandon, err := client.Do(requeteFormulaire(t, http.MethodPost,
		serveur.URL+"/scenarios/"+strconv.FormatInt(id, 10)+"/abandonner", jeton, nil))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(corps(t, repAbandon), "abandonné") {
		t.Fatal("le scénario abandonné devrait porter le badge abandonné")
	}
	sc, err = d.LireScenario(id)
	if err != nil {
		t.Fatal(err)
	}
	if sc.Statut != depot.ScenarioAbandonne {
		t.Fatalf("statut attendu ABANDONNE, obtenu %s", sc.Statut)
	}

	// reprise
	repReprise, err := client.Do(requeteFormulaire(t, http.MethodPost,
		serveur.URL+"/scenarios/"+strconv.FormatInt(id, 10)+"/reprendre", jeton, nil))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(corps(t, repReprise), "actif") {
		t.Fatal("le scénario repris devrait porter le badge actif")
	}
	sc, err = d.LireScenario(id)
	if err != nil {
		t.Fatal(err)
	}
	if sc.Statut != depot.ScenarioActif {
		t.Fatalf("statut attendu ACTIF après reprise, obtenu %s", sc.Statut)
	}
}

// TestScenarioPromotionAvecConcurrentAbandonne couvre le cas central de
// l'écran de promotion : le concurrent à abandonner est choisi explicitement
// (case cochée), jamais déduit. On vérifie via une seconde requête HTTP que
// ce concurrent apparaît bien ABANDONNE une fois la promotion appliquée.
func TestScenarioPromotionAvecConcurrentAbandonne(t *testing.T) {
	serveur, d := serveurDeTest(t)
	client := clientConnecte(t, serveur)

	principal, err := d.CreerScenario(depot.Scenario{Nom: "Principal", Description: "Le scénario qu'on retient"})
	if err != nil {
		t.Fatal(err)
	}
	concurrent, err := d.CreerScenario(depot.Scenario{Nom: "Concurrent", Description: "Hypothèse concurrente"})
	if err != nil {
		t.Fatal(err)
	}

	// la page de promotion doit lister le concurrent, non déduit mais présent
	// pour un choix explicite
	repPagePromo, err := client.Get(serveur.URL + "/scenarios/" + strconv.FormatInt(principal.ID, 10) + "/promouvoir")
	if err != nil {
		t.Fatal(err)
	}
	pagePromo := corps(t, repPagePromo)
	if !strings.Contains(pagePromo, "Concurrent") {
		t.Fatalf("le scénario concurrent devrait apparaître dans la page de promotion : %s", pagePromo)
	}
	jeton := csrfDepuisPage(t, pagePromo)

	champs := url.Values{"abandonner": {strconv.FormatInt(concurrent.ID, 10)}}
	repPromouvoir, err := client.Do(requeteFormulaire(t, http.MethodPost,
		serveur.URL+"/scenarios/"+strconv.FormatInt(principal.ID, 10)+"/promouvoir", jeton, champs))
	if err != nil {
		t.Fatal(err)
	}
	defer repPromouvoir.Body.Close()
	if repPromouvoir.StatusCode != http.StatusSeeOther {
		t.Fatalf("promotion réussie : attendu 303, obtenu %d", repPromouvoir.StatusCode)
	}
	if loc := repPromouvoir.Header.Get("Location"); loc != "/scenarios" {
		t.Fatalf("la redirection devrait viser /scenarios, obtenu %q", loc)
	}

	principalRelu, err := d.LireScenario(principal.ID)
	if err != nil {
		t.Fatal(err)
	}
	if principalRelu.Statut != depot.ScenarioRetenu {
		t.Fatalf("le scénario promu devrait être RETENU : %+v", principalRelu)
	}

	// seconde requête : le tableau (via HTTP) doit montrer le concurrent
	// abandonné
	repTableau, err := client.Get(serveur.URL + "/scenarios/tableau?inclure_inactifs=1")
	if err != nil {
		t.Fatal(err)
	}
	fragTableau := corps(t, repTableau)
	if !strings.Contains(fragTableau, "Concurrent") || !strings.Contains(fragTableau, "abandonné") {
		t.Fatalf("le concurrent devrait apparaître ABANDONNE dans le tableau : %s", fragTableau)
	}

	concurrentRelu, err := d.LireScenario(concurrent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if concurrentRelu.Statut != depot.ScenarioAbandonne {
		t.Fatalf("le scénario concurrent devrait être ABANDONNE : %+v", concurrentRelu)
	}
}

// TestScenarioPromotionEchoueSiDejaRetenu vérifie que promouvoir un scénario
// déjà RETENU échoue proprement : la page se réaffiche avec un message, pas
// de 4xx/5xx, pas de panique.
func TestScenarioPromotionEchoueSiDejaRetenu(t *testing.T) {
	serveur, d := serveurDeTest(t)
	client := clientConnecte(t, serveur)

	sc, err := d.CreerScenario(depot.Scenario{Nom: "Déjà retenu", Description: "Premier tour de promotion"})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Promouvoir(sc.ID, nil); err != nil {
		t.Fatalf("promotion initiale (par le dépôt) : %v", err)
	}

	repPage, err := client.Get(serveur.URL + "/scenarios/" + strconv.FormatInt(sc.ID, 10) + "/promouvoir")
	if err != nil {
		t.Fatal(err)
	}
	jeton := csrfDepuisPage(t, corps(t, repPage))

	rep, err := client.Do(requeteFormulaire(t, http.MethodPost,
		serveur.URL+"/scenarios/"+strconv.FormatInt(sc.ID, 10)+"/promouvoir", jeton, url.Values{}))
	if err != nil {
		t.Fatal(err)
	}
	if rep.StatusCode != http.StatusOK {
		t.Fatalf("échec métier : attendu 200 (page réaffichée), obtenu %d", rep.StatusCode)
	}
	frag := corps(t, rep)
	if !strings.Contains(frag, "non promouvable") {
		t.Fatalf("le message d'échec devrait apparaître dans la page réaffichée : %s", frag)
	}

	relu, err := d.LireScenario(sc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if relu.Statut != depot.ScenarioRetenu {
		t.Fatalf("le scénario devrait rester RETENU après l'échec : %+v", relu)
	}
}
