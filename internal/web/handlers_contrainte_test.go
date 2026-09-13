package web

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

// reMinTotal3, reMinTotal6 repèrent une ligne de tableau MIN_TOTAL suivie de
// sa valeur, quelle que soit l'indentation exacte du gabarit — la valeur
// "6" réapparaît aussi dans le bloc « effectif », donc un simple comptage de
// sous-chaînes sur toute la page ne distingue pas fiablement les lignes du
// tableau brut de ce bloc.
var (
	reMinTotal3 = regexp.MustCompile(`<td>MIN_TOTAL</td>\s*<td>3</td>`)
	reMinTotal6 = regexp.MustCompile(`<td>MIN_TOTAL</td>\s*<td>6</td>`)
)

// TestContraintesSectionDefinirEtLister vérifie le cas de base : poser
// MIN_TOTAL=3 dans le réel, et le retrouver dans le fragment chargé par
// hx-get.
func TestContraintesSectionDefinirEtLister(t *testing.T) {
	serveur, d := serveurDeTest(t)
	client := clientConnecte(t, serveur)
	base := d.Base()

	script := `
	INSERT INTO projet (id, code, libelle) VALUES (1, 'LOGS', 'Log Management');
	INSERT INTO environnement (id, code, libelle, ordre) VALUES (1, 'PROD', 'Production', 10);
	INSERT INTO techno (id, code, libelle) VALUES (1, 'ELASTIC', 'Elasticsearch');
	INSERT INTO cluster (id, nom, projet_id, environnement_id, techno_id) VALUES
		(1, 'ElasticHot', 1, 1, 1);
	`
	if _, err := base.Exec(script); err != nil {
		t.Fatalf("fixture : %v", err)
	}

	repPage, err := client.Get(serveur.URL + "/clusters/1/contraintes?annee=2027")
	if err != nil {
		t.Fatal(err)
	}
	if repPage.StatusCode != http.StatusOK {
		t.Fatalf("chargement initial : attendu 200, obtenu %d", repPage.StatusCode)
	}
	pageInitiale := corps(t, repPage)
	if !strings.Contains(pageInitiale, "Aucune contrainte") {
		t.Fatalf("section vide attendue au départ : %s", pageInitiale)
	}

	// le fragment de la section ne contient pas de jeton CSRF (il n'est
	// jamais chargé comme page complète) : on va le chercher sur une vraie
	// page pour l'obtenir, comme le fait csrfDepuisPage sur /clusters ou
	// /variables ailleurs.
	repClusters, err := client.Get(serveur.URL + "/clusters")
	if err != nil {
		t.Fatal(err)
	}
	jeton := csrfDepuisPage(t, corps(t, repClusters))

	champs := url.Values{
		"type":   {"MIN_TOTAL"},
		"valeur": {"3"},
	}
	repDef, err := client.Do(requeteFormulaire(t, http.MethodPost,
		serveur.URL+"/clusters/1/contraintes?annee=2027&scenario=0", jeton, champs))
	if err != nil {
		t.Fatal(err)
	}
	if repDef.StatusCode != http.StatusOK {
		t.Fatalf("définir : attendu 200, obtenu %d", repDef.StatusCode)
	}
	fragment := corps(t, repDef)
	if !strings.Contains(fragment, "MIN_TOTAL") || !strings.Contains(fragment, "3") {
		t.Fatalf("la contrainte définie devrait apparaître : %s", fragment)
	}
	if !strings.Contains(fragment, "badge-actif") || !strings.Contains(fragment, "réel") {
		t.Fatalf("une contrainte du réel doit porter le badge « réel » : %s", fragment)
	}

	// rechargement de la section : la contrainte persiste.
	repRelue, err := client.Get(serveur.URL + "/clusters/1/contraintes?annee=2027")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(corps(t, repRelue), "MIN_TOTAL") {
		t.Fatalf("la contrainte doit persister au rechargement")
	}
}

// TestContraintesSectionScenarioSurcharge prolonge la fixture précédente sous
// un scénario : MIN_TOTAL=3 dans le réel, MIN_TOTAL=6 sous le scénario 1.
// Le fragment sous scénario doit montrer les deux lignes avec leurs badges et
// un « effectif » à 6 (la surcharge l'emporte) ; le fragment réel ne doit
// montrer que 3.
func TestContraintesSectionScenarioSurcharge(t *testing.T) {
	serveur, d := serveurDeTest(t)
	client := clientConnecte(t, serveur)
	base := d.Base()

	script := `
	INSERT INTO projet (id, code, libelle) VALUES (1, 'LOGS', 'Log Management');
	INSERT INTO environnement (id, code, libelle, ordre) VALUES (1, 'PROD', 'Production', 10);
	INSERT INTO techno (id, code, libelle) VALUES (1, 'ELASTIC', 'Elasticsearch');
	INSERT INTO cluster (id, nom, projet_id, environnement_id, techno_id) VALUES
		(1, 'ElasticHot', 1, 1, 1);
	INSERT INTO scenario (id, nom, description, statut, date_creation)
		VALUES (1, 'Renfort HOT', 'test', 'ACTIF', '2026-01-01');
	INSERT INTO contrainte (cluster_id, scenario_id, annee, type, valeur)
		VALUES (1, NULL, 2027, 'MIN_TOTAL', 3);
	INSERT INTO contrainte (cluster_id, scenario_id, annee, type, valeur)
		VALUES (1, 1, 2027, 'MIN_TOTAL', 6);
	`
	if _, err := base.Exec(script); err != nil {
		t.Fatalf("fixture : %v", err)
	}

	// sous le scénario : les deux lignes, badges distincts, effectif à 6.
	repSc, err := client.Get(serveur.URL + "/clusters/1/contraintes?annee=2027&scenario=1")
	if err != nil {
		t.Fatal(err)
	}
	pageSc := corps(t, repSc)
	if !reMinTotal3.MatchString(pageSc) || !reMinTotal6.MatchString(pageSc) {
		t.Fatalf("les deux lignes MIN_TOTAL (réel à 3, scénario à 6) devraient apparaître : %s", pageSc)
	}
	if !strings.Contains(pageSc, "badge-actif") || !strings.Contains(pageSc, `<span class="badge">scénario</span>`) {
		t.Fatalf("les badges réel et scénario devraient tous deux apparaître : %s", pageSc)
	}
	if !strings.Contains(pageSc, "Effectif sous ce scénario") {
		t.Fatalf("le bloc effectif devrait apparaître sous un scénario : %s", pageSc)
	}
	if !strings.Contains(pageSc, "6") {
		t.Fatalf("l'effectif sous ce scénario devrait valoir 6 (la surcharge l'emporte) : %s", pageSc)
	}

	// sous le réel : une seule ligne, à 3, pas de bloc effectif.
	repReel, err := client.Get(serveur.URL + "/clusters/1/contraintes?annee=2027")
	if err != nil {
		t.Fatal(err)
	}
	pageReel := corps(t, repReel)
	if strings.Count(pageReel, "<td>MIN_TOTAL</td>") != 1 {
		t.Fatalf("le réel ne doit montrer que sa propre ligne MIN_TOTAL : %s", pageReel)
	}
	if !strings.Contains(pageReel, "<td>3</td>") {
		t.Fatalf("le réel doit afficher 3, pas la surcharge : %s", pageReel)
	}
	if strings.Contains(pageReel, "Effectif sous ce scénario") {
		t.Fatalf("le bloc effectif ne doit apparaître que sous un scénario : %s", pageReel)
	}
}

// TestContraintesEquilibrageDCSansPorteeMessage vérifie qu'EQUILIBRAGE_ZONE
// sans portée est refusé avec un message inline, jamais un 4xx/5xx (règle 2
// de la tâche : une erreur métier renvoie le fragment avec .Erreur renseigné).
func TestContraintesEquilibrageDCSansPorteeMessage(t *testing.T) {
	serveur, d := serveurDeTest(t)
	client := clientConnecte(t, serveur)
	base := d.Base()

	script := `
	INSERT INTO projet (id, code, libelle) VALUES (1, 'LOGS', 'Log Management');
	INSERT INTO environnement (id, code, libelle, ordre) VALUES (1, 'PROD', 'Production', 10);
	INSERT INTO techno (id, code, libelle) VALUES (1, 'ELASTIC', 'Elasticsearch');
	INSERT INTO cluster (id, nom, projet_id, environnement_id, techno_id) VALUES
		(1, 'ElasticHot', 1, 1, 1);
	`
	if _, err := base.Exec(script); err != nil {
		t.Fatalf("fixture : %v", err)
	}

	repClusters, err := client.Get(serveur.URL + "/clusters")
	if err != nil {
		t.Fatal(err)
	}
	jeton := csrfDepuisPage(t, corps(t, repClusters))

	champs := url.Values{"type": {"EQUILIBRAGE_ZONE"}}
	rep, err := client.Do(requeteFormulaire(t, http.MethodPost,
		serveur.URL+"/clusters/1/contraintes?annee=2027", jeton, champs))
	if err != nil {
		t.Fatal(err)
	}
	if rep.StatusCode != http.StatusOK {
		t.Fatalf("erreur métier : attendu 200, obtenu %d", rep.StatusCode)
	}
	fragment := corps(t, rep)
	if !strings.Contains(fragment, "portée") {
		t.Fatalf("le message d'erreur sur la portée manquante devrait apparaître : %s", fragment)
	}
	if !strings.Contains(fragment, "Aucune contrainte") {
		t.Fatalf("aucune contrainte n'aurait dû être créée : %s", fragment)
	}
}

// TestContraintesSupprimer vérifie qu'une contrainte supprimée disparaît du
// fragment rechargé.
func TestContraintesSupprimer(t *testing.T) {
	serveur, d := serveurDeTest(t)
	client := clientConnecte(t, serveur)
	base := d.Base()

	script := `
	INSERT INTO projet (id, code, libelle) VALUES (1, 'LOGS', 'Log Management');
	INSERT INTO environnement (id, code, libelle, ordre) VALUES (1, 'PROD', 'Production', 10);
	INSERT INTO techno (id, code, libelle) VALUES (1, 'ELASTIC', 'Elasticsearch');
	INSERT INTO cluster (id, nom, projet_id, environnement_id, techno_id) VALUES
		(1, 'ElasticHot', 1, 1, 1);
	INSERT INTO contrainte (id, cluster_id, scenario_id, annee, type, valeur)
		VALUES (1, 1, NULL, 2027, 'MIN_TOTAL', 3);
	`
	if _, err := base.Exec(script); err != nil {
		t.Fatalf("fixture : %v", err)
	}

	repClusters, err := client.Get(serveur.URL + "/clusters")
	if err != nil {
		t.Fatal(err)
	}
	jeton := csrfDepuisPage(t, corps(t, repClusters))

	rep, err := client.Do(requeteFormulaire(t, http.MethodPost,
		serveur.URL+"/clusters/1/contraintes/1/supprimer?annee=2027&scenario=0", jeton, url.Values{}))
	if err != nil {
		t.Fatal(err)
	}
	if rep.StatusCode != http.StatusOK {
		t.Fatalf("suppression : attendu 200, obtenu %d", rep.StatusCode)
	}
	fragment := corps(t, rep)
	if strings.Contains(fragment, "<td>MIN_TOTAL</td>") {
		t.Fatalf("la contrainte supprimée ne devrait plus apparaître : %s", fragment)
	}
	if !strings.Contains(fragment, "Aucune contrainte") {
		t.Fatalf("la section devrait redevenir vide : %s", fragment)
	}
}
