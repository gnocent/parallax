package web

import (
	"net/http"
	"strings"
	"testing"

	"parallax/internal/depot"
)

// fixtureComparaison plante deux clusters (ElasticHot/HOT, ElasticCold1/COLD)
// avec chacun un serveur réel EN_SERVICE, rattaché à une révision d'un modèle
// à prix fixé (18000 HT d'acquisition, 4200 HT annuel), puis un scénario ACTIF
// qui :
//   - ajoute un serveur HYPOTHESE affecté à ElasticHot (arrivée)
//   - retire PHY002 d'ElasticCold1 (l'affectation réelle reste ouverte, mais
//     le serveur est touché par le scénario via une ligne d'affectation dans
//     son seau, fermée aussitôt : « retiré, sans réaffectation », voir
//     internal/vues.ChargerParcCourant)
//
// Sous le réel (A) : ElasticHot=1 serveur, ElasticCold1=1 serveur.
// Sous le scénario (B) : ElasticHot=2 serveurs (PHY001 + HYP001),
// ElasticCold1=0 serveur (PHY002 touché, sans affectation active dans le
// seau du scénario).
func fixtureComparaison(t *testing.T, d *depot.Depot) {
	t.Helper()
	script := `
	INSERT INTO projet (id, code, libelle) VALUES (1, 'LOGS', 'Log Management');
	INSERT INTO environnement (id, code, libelle, ordre) VALUES (1, 'PROD', 'Production', 10);
	INSERT INTO techno (id, code, libelle) VALUES (1, 'ELASTIC', 'Elasticsearch');
	INSERT INTO tier (id, code, libelle, ordre) VALUES (1, 'HOT', 'Hot', 10), (2, 'COLD', 'Cold', 30);
	INSERT INTO cluster (id, nom, projet_id, environnement_id, techno_id, tier_id) VALUES
		(1, 'ElasticHot', 1, 1, 1, 1), (2, 'ElasticCold1', 1, 1, 1, 2);
	INSERT INTO modele (id, type, annee, code, prix_fournisseur_ht, cout_annuel_ht)
		VALUES (1, 'STD', 2020, 'STD-2020', 18000, 4200);
	INSERT INTO revision (id, modele_id, numero, date_effet) VALUES (1, 1, 1, '2020-01-01');
	INSERT INTO serveur (id, physical_name, statut, date_entree) VALUES
		(1, 'PHY001', 'EN_SERVICE', '2020-01-01'), (2, 'PHY002', 'EN_SERVICE', '2020-01-01');
	INSERT INTO serveur_revision (serveur_id, revision_id, date_debut) VALUES (1, 1, '2020-01-01'), (2, 1, '2020-01-01');
	INSERT INTO affectation (serveur_id, cluster_id, date_debut) VALUES (1, 1, '2020-01-01'), (2, 2, '2020-01-01');

	INSERT INTO scenario (id, nom, description, statut, date_creation) VALUES
		(1, 'Bascule test', 'Hypothèse de test pour la comparaison', 'ACTIF', '2024-01-01');

	INSERT INTO serveur (id, physical_name, statut, date_entree, scenario_id) VALUES
		(3, 'HYP001', 'HYPOTHESE', '2024-01-01', 1);
	INSERT INTO serveur_revision (serveur_id, revision_id, date_debut) VALUES (3, 1, '2024-01-01');
	INSERT INTO affectation (serveur_id, cluster_id, date_debut, scenario_id) VALUES (3, 1, '2024-01-01', 1);
	INSERT INTO affectation (serveur_id, cluster_id, date_debut, date_fin, scenario_id) VALUES
		(2, 2, '2020-01-01', '2024-01-01', 1);
	`
	if _, err := d.Base().Exec(script); err != nil {
		t.Fatalf("fixture : %v", err)
	}
}

func TestComparaisonResultatParCluster(t *testing.T) {
	serveur, d := serveurDeTest(t)
	client := clientConnecte(t, serveur)
	fixtureComparaison(t, d)

	// la page se charge, propose les deux sélecteurs et les valeurs de filtre
	// issues des deux côtés (HOT vient du réel, mais ElasticCold1 doit rester
	// visible même s'il n'a plus de serveur côté scénario)
	repPage, err := client.Get(serveur.URL + "/comparaison")
	if err != nil {
		t.Fatal(err)
	}
	page := corps(t, repPage)
	if !strings.Contains(page, `name="scenario_a"`) || !strings.Contains(page, `name="scenario_b"`) {
		t.Fatalf("la page devrait proposer deux sélecteurs de scénario : %s", page)
	}
	if !strings.Contains(page, "Bascule test") {
		t.Fatalf("le scénario devrait apparaître dans les sélecteurs : %s", page)
	}
	if !strings.Contains(page, "ElasticHot") || !strings.Contains(page, "ElasticCold1") {
		t.Fatalf("la page devrait lister les valeurs de filtre des deux côtés : %s", page)
	}

	// résultat groupé par cluster, A = réel, B = scénario
	url := serveur.URL + "/comparaison/resultat?axe1=cluster&scenario_b=1" +
		"&colonne=nb_serveurs&colonne=cout_acquisition&colonne=cout_annuel"
	repResultat, err := client.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	if repResultat.StatusCode != http.StatusOK {
		t.Fatalf("attendu 200, obtenu %d", repResultat.StatusCode)
	}
	resultat := corps(t, repResultat)

	if !strings.Contains(resultat, "ElasticHot") || !strings.Contains(resultat, "ElasticCold1") {
		t.Fatalf("le résultat devrait montrer les deux clusters : %s", resultat)
	}
	if !strings.Contains(resultat, "Réel") || !strings.Contains(resultat, "Bascule test") {
		t.Fatalf("A et B devraient être nommés Réel et Bascule test : %s", resultat)
	}

	// coût d'acquisition et coût annuel apparaissent quand cochés
	if !strings.Contains(resultat, "Coût d&#39;acquisition") && !strings.Contains(resultat, "Coût d'acquisition") {
		t.Fatalf("le coût d'acquisition devrait apparaître dans l'en-tête : %s", resultat)
	}
	if !strings.Contains(resultat, "Coût annuel") {
		t.Fatalf("le coût annuel devrait apparaître dans l'en-tête : %s", resultat)
	}

	// ElasticHot : A=1, B=2 (PHY001 + HYP001) -> nb_serveurs affiche 1 et 2 ;
	// ElasticCold1 : A=1, B=0 (PHY002 retiré par le scénario). Les groupes
	// sont triés par clé (Regrouper) : "ElasticCold1" précède "ElasticHot".
	iCold := strings.Index(resultat, "ElasticCold1")
	iHot := strings.Index(resultat, "ElasticHot")
	if iHot < 0 || iCold < 0 || iHot < iCold {
		t.Fatalf("clusters absents ou dans un ordre inattendu : %s", resultat)
	}
	finTbody := strings.Index(resultat, "</tbody>")
	if finTbody < 0 {
		finTbody = len(resultat)
	}
	ligneHot := resultat[iHot:finTbody]
	if !strings.Contains(ligneHot, ">1<") || !strings.Contains(ligneHot, ">2<") {
		t.Fatalf("ElasticHot devrait montrer A=1 et B=2 : %s", ligneHot)
	}

	// coûts d'acquisition : ElasticHot passe de 18000 à 36000 (Δ=18000) ;
	// coûts annuels : de 4200 à 8400 (Δ=4200)
	if !strings.Contains(ligneHot, "18000") || !strings.Contains(ligneHot, "36000") {
		t.Fatalf("ElasticHot devrait montrer les coûts d'acquisition 18000 et 36000 : %s", ligneHot)
	}
	if !strings.Contains(ligneHot, "4200") || !strings.Contains(ligneHot, "8400") {
		t.Fatalf("ElasticHot devrait montrer les coûts annuels 4200 et 8400 : %s", ligneHot)
	}

	// le total apparaît en bas de tableau
	if !strings.Contains(resultat, "Total") {
		t.Fatalf("le résultat devrait porter une ligne Total : %s", resultat)
	}

	// A = B est autorisé : comparer le réel à lui-même donne un delta nul
	// partout, pas une erreur
	repMemeScenario, err := client.Get(serveur.URL +
		"/comparaison/resultat?axe1=cluster&colonne=nb_serveurs")
	if err != nil {
		t.Fatal(err)
	}
	if repMemeScenario.StatusCode != http.StatusOK {
		t.Fatalf("A=B : attendu 200, obtenu %d", repMemeScenario.StatusCode)
	}
	memeScenario := corps(t, repMemeScenario)
	if strings.Contains(memeScenario, "ecart-positif") || strings.Contains(memeScenario, "ecart-negatif") {
		t.Fatalf("A=B ne devrait mettre aucun écart en évidence : %s", memeScenario)
	}

	// export CSV : en-têtes "<colonne> A", "<colonne> B", "<colonne> Δ"
	repCSV, err := client.Get(serveur.URL +
		"/comparaison/resultat.csv?axe1=cluster&scenario_b=1&colonne=nb_serveurs")
	if err != nil {
		t.Fatal(err)
	}
	csvCorps := corps(t, repCSV)
	if !strings.Contains(repCSV.Header.Get("Content-Disposition"), "attachment") {
		t.Fatalf("l'export CSV doit se télécharger (Content-Disposition), obtenu %q", repCSV.Header.Get("Content-Disposition"))
	}
	if !strings.HasPrefix(csvCorps, "Cluster;Nb serveurs A;Nb serveurs B;Nb serveurs Δ") {
		t.Fatalf("le CSV devrait commencer par les en-têtes attendues : %q", csvCorps)
	}
	if !strings.Contains(csvCorps, "ElasticHot;1;2;1") {
		t.Fatalf("le CSV devrait montrer ElasticHot avec A=1, B=2, Δ=1 : %q", csvCorps)
	}
	if !strings.Contains(csvCorps, "ElasticCold1;1;0;-1") {
		t.Fatalf("le CSV devrait montrer ElasticCold1 avec A=1, B=0, Δ=-1 : %q", csvCorps)
	}

	// export Excel : content-type OOXML
	repXLSX, err := client.Get(serveur.URL +
		"/comparaison/resultat.xlsx?axe1=cluster&scenario_b=1&colonne=nb_serveurs")
	if err != nil {
		t.Fatal(err)
	}
	defer repXLSX.Body.Close()
	if ct := repXLSX.Header.Get("Content-Type"); !strings.Contains(ct, "spreadsheetml") {
		t.Fatalf("content-type xlsx inattendu : %s", ct)
	}
}

// TestComparaisonColonnesLicences (v3.3) : les colonnes de licences se
// comparent comme les autres, chaque côté avec ses propres contrats — ici
// un contrat réel GLOBAL et sa surcharge MACHINE dans le scénario. Sans axe
// techno, l'axe est ajouté en fin ; la note GLOBAL et les boutons d'export
// accompagnent le résultat.
func TestComparaisonColonnesLicences(t *testing.T) {
	serveur, d := serveurDeTest(t)
	client := clientConnecte(t, serveur)
	fixtureComparaison(t, d)
	if _, err := d.Base().Exec(`
		INSERT INTO composant (revision_id, nature, code, quantite, capacite_unitaire, unite) VALUES (1, 'RAM', 'ram', 1, 512, 'GO');
		INSERT INTO modele_noeud (revision_id, techno_id, nb_noeuds) VALUES (1, 1, 1);
		INSERT INTO licence_contrat (techno_id, annee, scenario_id, mecanisme, niveau, ram_max_go) VALUES
			(1, 2024, NULL, 'RAM', 'GLOBAL', 256),
			(1, 2024, 1, 'RAM', 'MACHINE', 128);`); err != nil {
		t.Fatal(err)
	}

	// A (réel, GLOBAL, 256 Go) : ElasticHot 512 Go → 2, ElasticCold1 512 Go → 2
	// B (scénario, MACHINE, 128 Go) : ElasticHot 2 × 512 Go → 8, ElasticCold1 vide → 0
	rep, err := client.Get(serveur.URL + "/comparaison/resultat?axe1=cluster&scenario_b=1&date=2026-01-01&colonne=licences_unites")
	if err != nil {
		t.Fatal(err)
	}
	resultat := corps(t, rep)
	if rep.StatusCode != http.StatusOK {
		t.Fatalf("attendu 200, obtenu %d : %s", rep.StatusCode, resultat)
	}
	if !strings.Contains(resultat, "<th>Cluster</th><th>Techno</th>") {
		t.Fatalf("l'axe techno devrait être ajouté en fin : %s", resultat)
	}
	if !strings.Contains(resultat, "Niveau global") {
		t.Fatalf("un contrat GLOBAL d'un côté devrait afficher la note : %s", resultat)
	}
	if !strings.Contains(resultat, `href="/comparaison/resultat.csv?axe1=cluster`) {
		t.Fatalf("les boutons d'export du résultat devraient viser les routes dédiées : %s", resultat)
	}

	repCSV, err := client.Get(serveur.URL + "/comparaison/resultat.csv?axe1=cluster&scenario_b=1&date=2026-01-01&colonne=licences_unites")
	if err != nil {
		t.Fatal(err)
	}
	csvCorps := corps(t, repCSV)
	if !strings.HasPrefix(csvCorps, "Cluster;Techno;Unités de licence A;Unités de licence B;Unités de licence Δ\n") {
		t.Fatalf("en-têtes CSV inattendues : %q", csvCorps)
	}
	if !strings.Contains(csvCorps, "ElasticHot;ELASTIC;2;8;6\n") || !strings.Contains(csvCorps, "ElasticCold1;ELASTIC;2;0;-2\n") {
		t.Fatalf("valeurs CSV inattendues : %q", csvCorps)
	}
}
