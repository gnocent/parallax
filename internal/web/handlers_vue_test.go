package web

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestVuesConstructeurEtExport(t *testing.T) {
	serveur, d := serveurDeTest(t)
	client := clientConnecte(t, serveur)

	base := d.Base()
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
	INSERT INTO composant (revision_id, nature, code, quantite, capacite_unitaire, unite)
		VALUES (1, 'CPU', 'cpu', 2, 20, 'CORE');
	INSERT INTO serveur (id, physical_name, statut, date_entree) VALUES
		(1, 'PHY001', 'EN_SERVICE', '2020-01-01'), (2, 'PHY002', 'EN_SERVICE', '2020-01-01');
	INSERT INTO serveur_revision (serveur_id, revision_id, date_debut) VALUES (1, 1, '2020-01-01');
	INSERT INTO affectation (serveur_id, cluster_id, date_debut) VALUES (1, 1, '2020-01-01'), (2, 2, '2020-01-01');
	`
	if _, err := base.Exec(script); err != nil {
		t.Fatalf("fixture : %v", err)
	}

	// la page se charge et propose bien les valeurs de filtre issues du parc
	repPage, err := client.Get(serveur.URL + "/vues")
	if err != nil {
		t.Fatal(err)
	}
	page := corps(t, repPage)
	if !strings.Contains(page, "HOT") || !strings.Contains(page, "ElasticHot") {
		t.Fatalf("la page constructeur devrait lister les valeurs de filtre disponibles : %s", page)
	}

	// génération d'un résultat groupé par tier
	repResultat, err := client.Get(serveur.URL + "/vues/resultat?axe1=tier&colonne=nb_serveurs&colonne=cpu")
	if err != nil {
		t.Fatal(err)
	}
	resultat := corps(t, repResultat)
	if repResultat.StatusCode != http.StatusOK {
		t.Fatalf("attendu 200, obtenu %d : %s", repResultat.StatusCode, resultat)
	}
	if !strings.Contains(resultat, "HOT") || !strings.Contains(resultat, "COLD") {
		t.Fatalf("le résultat devrait montrer les deux tiers : %s", resultat)
	}
	if !strings.Contains(resultat, "40") { // 2 x 20 cœurs sur PHY001 (HOT)
		t.Fatalf("le résultat devrait montrer 40 cœurs cumulés pour HOT : %s", resultat)
	}

	// export CSV : content-type et en-tête de colonnes présents
	repCSV, err := client.Get(serveur.URL + "/vues/resultat.csv?axe1=tier&colonne=nb_serveurs")
	if err != nil {
		t.Fatal(err)
	}
	csvCorps := corps(t, repCSV)
	if !strings.Contains(repCSV.Header.Get("Content-Disposition"), "attachment") {
		t.Fatalf("l'export CSV doit se télécharger (Content-Disposition), obtenu %q", repCSV.Header.Get("Content-Disposition"))
	}
	if !strings.Contains(csvCorps, "Tier") {
		t.Fatalf("le CSV devrait contenir l'en-tête Tier : %s", csvCorps)
	}

	// export Excel : content-type OOXML, corps non vide (contenu binaire zip)
	repXLSX, err := client.Get(serveur.URL + "/vues/resultat.xlsx?axe1=tier&colonne=nb_serveurs")
	if err != nil {
		t.Fatal(err)
	}
	defer repXLSX.Body.Close()
	if ct := repXLSX.Header.Get("Content-Type"); !strings.Contains(ct, "spreadsheetml") {
		t.Fatalf("content-type xlsx inattendu : %s", ct)
	}

	// enregistrement d'une vue : jeton CSRF requis (extrait de la page comme
	// dans TestParcoursProjetCompletParHTTP)
	i := strings.Index(page, `X-CSRF-Token":"`) + len(`X-CSRF-Token":"`)
	jeton := page[i : strings.Index(page[i:], `"`)+i]

	form := url.Values{"axe1": {"tier"}, "colonne": {"nb_serveurs"}, "nom": {"Serveurs par tier"}}
	req, _ := http.NewRequest(http.MethodPost, serveur.URL+"/vues", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-CSRF-Token", jeton)
	repSave, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if repSave.StatusCode != http.StatusOK {
		t.Fatalf("enregistrement : attendu 200, obtenu %d", repSave.StatusCode)
	}
	if fragment := corps(t, repSave); !strings.Contains(fragment, "Serveurs par tier") {
		t.Fatalf("la vue enregistrée devrait apparaître dans le fragment : %s", fragment)
	}

	vuesEnregistrees, err := d.ListerVuesAccessibles(1)
	if err != nil {
		t.Fatal(err)
	}
	if len(vuesEnregistrees) != 1 || vuesEnregistrees[0].Nom != "Serveurs par tier" {
		t.Fatalf("la vue aurait dû être persistée : %+v", vuesEnregistrees)
	}

	// consultation de la vue enregistrée
	repOuverte, err := client.Get(serveur.URL + "/vues/1")
	if err != nil {
		t.Fatal(err)
	}
	ouverte := corps(t, repOuverte)
	if !strings.Contains(ouverte, "Serveurs par tier") || !strings.Contains(ouverte, "HOT") {
		t.Fatalf("la page de la vue enregistrée devrait montrer son nom et son résultat : %s", ouverte)
	}
	if !strings.Contains(ouverte, `href="/vues/1/resultat.csv`) {
		t.Fatalf("les liens d'export d'une vue enregistrée doivent viser ses propres routes : %s", ouverte)
	}

	// export d'une vue enregistrée : SA configuration (axe tier, nb_serveurs),
	// pas une configuration vide — c'était le bug : les liens pointaient sur
	// /vues/resultat.csv sans aucun paramètre.
	repCSVVue, err := client.Get(serveur.URL + "/vues/1/resultat.csv")
	if err != nil {
		t.Fatal(err)
	}
	csvVue := corps(t, repCSVVue)
	if !strings.HasPrefix(csvVue, "Tier;Nb serveurs") || !strings.Contains(csvVue, "HOT;1") || !strings.Contains(csvVue, "COLD;1") {
		t.Fatalf("l'export d'une vue enregistrée doit refléter sa configuration : %q", csvVue)
	}
	repXLSXVue, err := client.Get(serveur.URL + "/vues/1/resultat.xlsx")
	if err != nil {
		t.Fatal(err)
	}
	repXLSXVue.Body.Close()
	if ct := repXLSXVue.Header.Get("Content-Type"); !strings.Contains(ct, "spreadsheetml") {
		t.Fatalf("export xlsx d'une vue enregistrée : content-type inattendu : %s", ct)
	}
	if rep404, _ := client.Get(serveur.URL + "/vues/999/resultat.csv"); rep404.StatusCode != http.StatusNotFound {
		t.Fatalf("vue inexistante : attendu 404, obtenu %d", rep404.StatusCode)
	}
}
