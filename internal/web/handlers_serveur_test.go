package web

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"parallax/internal/depot"
)

func seedServeurFixtures(t *testing.T, d *depot.Depot) (dcID int64, clusterA, clusterB depot.Cluster) {
	t.Helper()
	dc, err := d.CreerZone(depot.Zone{Code: "DC1", Libelle: "Zone 1"})
	if err != nil {
		t.Fatalf("seed zone : %v", err)
	}
	projetA, err := d.CreerProjet(depot.Projet{Code: "LOGS", Libelle: "Log Management"})
	if err != nil {
		t.Fatalf("seed projet A : %v", err)
	}
	projetB, err := d.CreerProjet(depot.Projet{Code: "STREAM", Libelle: "Stream"})
	if err != nil {
		t.Fatalf("seed projet B : %v", err)
	}
	env, err := d.CreerEnvironnement(depot.Environnement{Code: "PROD", Libelle: "Production", Ordre: 1})
	if err != nil {
		t.Fatalf("seed environnement : %v", err)
	}
	techno, err := d.CreerTechno(depot.Techno{Code: "ELASTIC", Libelle: "Elasticsearch"})
	if err != nil {
		t.Fatalf("seed techno : %v", err)
	}
	clA, err := d.CreerCluster(depot.Cluster{Nom: "ElasticHot1", ProjetID: projetA.ID, EnvironnementID: env.ID, TechnoID: techno.ID})
	if err != nil {
		t.Fatalf("seed cluster A : %v", err)
	}
	clB, err := d.CreerCluster(depot.Cluster{Nom: "StreamKafka1", ProjetID: projetB.ID, EnvironnementID: env.ID, TechnoID: techno.ID})
	if err != nil {
		t.Fatalf("seed cluster B : %v", err)
	}
	return dc.ID, clA, clB
}

// TestParcoursServeurCompletParHTTP est le test clé du critère d'acceptation
// v1.3 : un serveur affecté au cluster (donc au projet) A, puis réaffecté au
// cluster (donc au projet) B, conserve son historique complet — la première
// affectation apparaît fermée, la seconde ouverte. Couvre aussi le refus
// d'une seconde affectation active (invariant 3) exposé comme une erreur
// métier affichée en 200, jamais 4xx.
func TestParcoursServeurCompletParHTTP(t *testing.T) {
	serveur, d := serveurDeTest(t)
	client := clientConnecte(t, serveur)
	dcID, clusterA, clusterB := seedServeurFixtures(t, d)

	repPage, err := client.Get(serveur.URL + "/serveurs")
	if err != nil {
		t.Fatal(err)
	}
	jeton := csrfDepuisPage(t, corps(t, repPage))

	// création d'un serveur
	repCreation, err := client.Do(requeteFormulaire(t, http.MethodPost, serveur.URL+"/serveurs", jeton, url.Values{
		"physical_name": {"PHY001"},
		"hostname":      {"esh01"},
		"zone_id":       {strconv.FormatInt(dcID, 10)},
		"statut":        {depot.StatutEnService},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if repCreation.StatusCode != http.StatusOK {
		t.Fatalf("création : attendu 200, obtenu %d", repCreation.StatusCode)
	}
	if frag := corps(t, repCreation); !strings.Contains(frag, "PHY001") {
		t.Fatalf("le serveur créé devrait apparaître dans le tableau : %s", frag)
	}

	serveurs, err := d.ListerServeurs(depot.FiltreServeur{})
	if err != nil {
		t.Fatal(err)
	}
	if len(serveurs) != 1 {
		t.Fatalf("attendu 1 serveur en base, obtenu %d", len(serveurs))
	}
	srvID := serveurs[0].ID

	// page de détail accessible, montre "aucune affectation active"
	repDetail, err := client.Get(serveur.URL + "/serveurs/" + strconv.FormatInt(srvID, 10))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(corps(t, repDetail), "Aucune affectation active") {
		t.Fatal("un serveur neuf ne devrait avoir aucune affectation active")
	}

	urlServeur := serveur.URL + "/serveurs/" + strconv.FormatInt(srvID, 10)

	// affectation initiale au cluster A (projet Logs)
	repAffect, err := client.Do(requeteFormulaire(t, http.MethodPost, urlServeur+"/affecter", jeton, url.Values{
		"cluster_id": {strconv.FormatInt(clusterA.ID, 10)},
		"date_debut": {"2020-01-01"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if repAffect.StatusCode != http.StatusOK {
		t.Fatalf("affectation : attendu 200, obtenu %d", repAffect.StatusCode)
	}
	if frag := corps(t, repAffect); !strings.Contains(frag, "ElasticHot1") {
		t.Fatalf("le cluster affecté devrait apparaître : %s", frag)
	}

	// refus d'une seconde affectation active (invariant 3) : 200, message inline
	repRefus, err := client.Do(requeteFormulaire(t, http.MethodPost, urlServeur+"/affecter", jeton, url.Values{
		"cluster_id": {strconv.FormatInt(clusterB.ID, 10)},
		"date_debut": {"2020-06-01"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if repRefus.StatusCode != http.StatusOK {
		t.Fatalf("refus métier : attendu 200 (jamais 4xx sur une erreur métier htmx), obtenu %d", repRefus.StatusCode)
	}
	fragRefus := corps(t, repRefus)
	if !strings.Contains(fragRefus, "affectation active") {
		t.Fatalf("le message de chevauchement devrait apparaître : %s", fragRefus)
	}
	if !strings.Contains(fragRefus, "ElasticHot1") {
		t.Fatal("l'affectation active courante (cluster A) doit rester affichée après le refus")
	}

	// réaffectation vers le cluster B (projet Stream) : la migration = réaffectation
	repReaffect, err := client.Do(requeteFormulaire(t, http.MethodPost, urlServeur+"/reaffecter", jeton, url.Values{
		"cluster_id": {strconv.FormatInt(clusterB.ID, 10)},
		"date_debut": {"2024-01-01"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if repReaffect.StatusCode != http.StatusOK {
		t.Fatalf("réaffectation : attendu 200, obtenu %d", repReaffect.StatusCode)
	}
	fragReaffect := corps(t, repReaffect)
	if !strings.Contains(fragReaffect, "StreamKafka1") {
		t.Fatalf("le nouveau cluster devrait apparaître comme affectation active : %s", fragReaffect)
	}

	// critère d'acceptation v1.3 : l'historique complet est conservé
	historique, err := d.ListerAffectationsServeur(srvID)
	if err != nil {
		t.Fatal(err)
	}
	if len(historique) != 2 {
		t.Fatalf("l'historique complet doit compter 2 vies après réaffectation, obtenu %+v", historique)
	}
	if historique[0].ClusterID != clusterA.ID || historique[0].DateFin == nil {
		t.Fatalf("1re vie : cluster A, fermée : %+v", historique[0])
	}
	if historique[1].ClusterID != clusterB.ID || historique[1].DateFin != nil {
		t.Fatalf("2e vie : cluster B, ouverte : %+v", historique[1])
	}

	// la page de détail reflète les deux vies dans son historique HTML
	repDetail2, err := client.Get(urlServeur)
	if err != nil {
		t.Fatal(err)
	}
	corpsDetail := corps(t, repDetail2)
	if !strings.Contains(corpsDetail, "ElasticHot1") || !strings.Contains(corpsDetail, "StreamKafka1") {
		t.Fatalf("la page de détail doit montrer les deux clusters dans l'historique : %s", corpsDetail)
	}

	// désaffectation
	repDesaffect, err := client.Do(requeteFormulaire(t, http.MethodPost, urlServeur+"/desaffecter", jeton, url.Values{
		"date_fin": {"2025-12-31"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(corps(t, repDesaffect), "Aucune affectation active") {
		t.Fatal("après désaffectation, plus aucune affectation active ne devrait être annoncée")
	}
}

// TestServeursSansAffectationParHTTP couvre le point 4 de la tâche : la vue
// "sans affectation active" liste un serveur neuf, et l'affectation rapide
// depuis cette vue le retire de la liste (choix documenté dans
// serveurs/sans_affectation.html : réponse vide -> la ligne disparaît).
func TestServeursSansAffectationParHTTP(t *testing.T) {
	serveur, d := serveurDeTest(t)
	client := clientConnecte(t, serveur)
	_, clusterA, _ := seedServeurFixtures(t, d)

	srv, err := d.CreerServeur(depot.Serveur{Statut: depot.StatutEnService})
	if err != nil {
		t.Fatal(err)
	}

	repPage, err := client.Get(serveur.URL + "/serveurs/sans-affectation")
	if err != nil {
		t.Fatal(err)
	}
	corpsPage := corps(t, repPage)
	if !strings.Contains(corpsPage, "#"+strconv.FormatInt(srv.ID, 10)) {
		t.Fatalf("le serveur neuf devrait apparaître comme sans affectation active : %s", corpsPage)
	}

	repProjets, err := client.Get(serveur.URL + "/projets")
	if err != nil {
		t.Fatal(err)
	}
	jeton := csrfDepuisPage(t, corps(t, repProjets))

	repAffect, err := client.Do(requeteFormulaire(t, http.MethodPost,
		serveur.URL+"/serveurs/sans-affectation/"+strconv.FormatInt(srv.ID, 10)+"/affecter", jeton, url.Values{
			"cluster_id": {strconv.FormatInt(clusterA.ID, 10)},
			"date_debut": {"2024-01-01"},
		}))
	if err != nil {
		t.Fatal(err)
	}
	if repAffect.StatusCode != http.StatusOK {
		t.Fatalf("affectation rapide : attendu 200, obtenu %d", repAffect.StatusCode)
	}
	if frag := corps(t, repAffect); strings.TrimSpace(frag) != "" {
		t.Fatalf("le succès doit renvoyer un corps vide (la ligne disparaît) : %q", frag)
	}

	items, err := d.ListerServeursSansAffectationActive(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range items {
		if it.Serveur.ID == srv.ID {
			t.Fatal("le serveur affecté ne devrait plus figurer dans la liste sans affectation active")
		}
	}
}

// TestCreerServeurHypothetiqueParHTTP couvre la création d'un serveur
// HYPOTHESE (v2.1) depuis le formulaire de la liste : sans scénario choisi,
// HYPOTHESE est refusé (invariant 5, affiché comme une erreur métier, jamais
// une 4xx/5xx) ; avec un scénario, la création réussit et le serveur créé
// reste visible dans le tableau réaffiché malgré le filtre réel par défaut.
func TestCreerServeurHypothetiqueParHTTP(t *testing.T) {
	serveur, d := serveurDeTest(t)
	client := clientConnecte(t, serveur)

	if _, err := d.Base().Exec(
		`INSERT INTO scenario (id, nom, description, statut, date_creation)
		 VALUES (1, 'Renfort', 'test', 'ACTIF', '2026-01-01')`); err != nil {
		t.Fatalf("seed scénario : %v", err)
	}

	repPage, err := client.Get(serveur.URL + "/serveurs")
	if err != nil {
		t.Fatal(err)
	}
	jeton := csrfDepuisPage(t, corps(t, repPage))

	// HYPOTHESE sans scénario : refusé, mais 200 et message inline.
	repRefus, err := client.Do(requeteFormulaire(t, http.MethodPost, serveur.URL+"/serveurs", jeton, url.Values{
		"physical_name": {"PHYHYP"},
		"statut":        {depot.StatutHypothese},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if repRefus.StatusCode != http.StatusOK {
		t.Fatalf("refus HYPOTHESE sans scénario : attendu 200, obtenu %d", repRefus.StatusCode)
	}
	if serveurs, _ := d.ListerServeurs(depot.FiltreServeur{}); len(serveurs) != 0 {
		t.Fatalf("aucun serveur ne devrait avoir été créé : %+v", serveurs)
	}

	// HYPOTHESE avec scénario : réussit, et reste visible dans le tableau
	// réaffiché (le filtre réel, par défaut, ne l'aurait pas montré).
	repCreation, err := client.Do(requeteFormulaire(t, http.MethodPost, serveur.URL+"/serveurs", jeton, url.Values{
		"physical_name": {"PHYHYP"},
		"statut":        {depot.StatutHypothese},
		"scenario_id":   {"1"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if repCreation.StatusCode != http.StatusOK {
		t.Fatalf("création HYPOTHESE avec scénario : attendu 200, obtenu %d", repCreation.StatusCode)
	}
	if frag := corps(t, repCreation); !strings.Contains(frag, "PHYHYP") {
		t.Fatalf("le serveur hypothétique créé doit rester visible dans le tableau réaffiché : %s", frag)
	}

	serveurs, err := d.ListerServeurs(depot.FiltreServeur{})
	if err != nil {
		t.Fatal(err)
	}
	if len(serveurs) != 0 {
		t.Fatalf("le réel ne doit toujours montrer aucun serveur : %+v", serveurs)
	}
	sc := int64(1)
	avecSc, err := d.ListerServeurs(depot.FiltreServeur{ScenarioID: &sc})
	if err != nil {
		t.Fatal(err)
	}
	if len(avecSc) != 1 || avecSc[0].Statut != depot.StatutHypothese {
		t.Fatalf("le serveur hypothétique doit apparaître sous le scénario : %+v", avecSc)
	}

	// ouvert sans paramètre, le détail se place de lui-même sur le scénario
	// du serveur (sinon « aucune affectation active » serait trompeur).
	repDetail, err := client.Get(serveur.URL + "/serveurs/" + strconv.FormatInt(avecSc[0].ID, 10))
	if err != nil {
		t.Fatal(err)
	}
	if page := corps(t, repDetail); !strings.Contains(page, "sous ce scénario") || !strings.Contains(page, `value="1" selected`) {
		t.Fatalf("le détail d'un serveur hypothétique doit s'ouvrir sur son scénario : %s", page)
	}
}

// TestParcoursServeurScenarioParHTTP couvre, sur la section affectation de la
// page de détail, les deux gestes propres à un scénario (déplacer, retirer) —
// v2.1 du backlog — et vérifie que le réel n'est jamais modifié tant qu'il
// n'y a pas de promotion.
func TestParcoursServeurScenarioParHTTP(t *testing.T) {
	serveur, d := serveurDeTest(t)
	client := clientConnecte(t, serveur)
	dcID, clusterA, clusterB := seedServeurFixtures(t, d)
	_ = dcID

	if _, err := d.Base().Exec(
		`INSERT INTO scenario (id, nom, description, statut, date_creation)
		 VALUES (1, 'Renfort', 'test', 'ACTIF', '2026-01-01')`); err != nil {
		t.Fatalf("seed scénario : %v", err)
	}

	repPage, err := client.Get(serveur.URL + "/serveurs")
	if err != nil {
		t.Fatal(err)
	}
	jeton := csrfDepuisPage(t, corps(t, repPage))

	repCreation, err := client.Do(requeteFormulaire(t, http.MethodPost, serveur.URL+"/serveurs", jeton, url.Values{
		"physical_name": {"PHYSC"},
		"statut":        {depot.StatutEnService},
	}))
	if err != nil {
		t.Fatal(err)
	}
	_ = corps(t, repCreation)
	serveurs, err := d.ListerServeurs(depot.FiltreServeur{})
	if err != nil {
		t.Fatal(err)
	}
	srvID := serveurs[len(serveurs)-1].ID
	urlServeur := serveur.URL + "/serveurs/" + strconv.FormatInt(srvID, 10)

	// affectation réelle initiale au cluster A.
	repAffect, err := client.Do(requeteFormulaire(t, http.MethodPost, urlServeur+"/affecter", jeton, url.Values{
		"cluster_id": {strconv.FormatInt(clusterA.ID, 10)},
		"date_debut": {"2020-01-01"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	_ = corps(t, repAffect)

	// sous le scénario : déplacer vers le cluster B (Réaffecter, scenario=1).
	repDeplace, err := client.Do(requeteFormulaire(t, http.MethodPost, urlServeur+"/reaffecter", jeton, url.Values{
		"cluster_id": {strconv.FormatInt(clusterB.ID, 10)},
		"date_debut": {"2026-02-01"},
		"scenario":   {"1"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if frag := corps(t, repDeplace); !strings.Contains(frag, "StreamKafka1") {
		t.Fatalf("sous le scénario, le serveur doit apparaître sur StreamKafka1 : %s", frag)
	}

	// le réel n'a pas bougé.
	repReel, err := client.Get(urlServeur)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(corps(t, repReel), "ElasticHot1") {
		t.Fatal("le réel doit rester sur ElasticHot1, inchangé par le scénario")
	}

	// sous le scénario, à nouveau : retirer (Desaffecter, scenario=1).
	repRetrait, err := client.Do(requeteFormulaire(t, http.MethodPost, urlServeur+"/desaffecter", jeton, url.Values{
		"date_fin": {"2026-06-01"},
		"scenario": {"1"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if frag := corps(t, repRetrait); !strings.Contains(frag, "Aucune affectation active sous ce scénario") {
		t.Fatalf("après retrait, plus aucune affectation effective sous le scénario : %s", frag)
	}

	// le réel, lui, est toujours sur ElasticHot1 : le retrait ne l'a pas touché.
	vies, err := d.ListerAffectationsServeur(srvID)
	if err != nil {
		t.Fatal(err)
	}
	var reelActif bool
	for _, v := range vies {
		if v.ScenarioID == nil && v.DateFin == nil && v.ClusterID == clusterA.ID {
			reelActif = true
		}
	}
	if !reelActif {
		t.Fatalf("le réel doit rester actif sur clusterA : %+v", vies)
	}
}
