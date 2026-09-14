package web

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"parallax/internal/depot"
)

// fixtureSynthese reprend celle du dimensionnement inverse (un cluster
// ElasticHot, STD-2020 installé à 96 To, candidat DENSE-2027 à 576 To, règle
// « Disque HOT » à 35 To en 2027, scénario 1 actif sans projet) et lui
// ajoute un second cluster sans tier ni règle, pour vérifier qu'il apparaît
// dans la synthèse sans casser le lot.
func fixtureSynthese(t *testing.T, d *depot.Depot) {
	t.Helper()
	script := `
	INSERT INTO projet (id, code, libelle) VALUES (1, 'LOGS', 'Log Management');
	INSERT INTO environnement (id, code, libelle, ordre) VALUES (1, 'PROD', 'Production', 10);
	INSERT INTO techno (id, code, libelle) VALUES (1, 'ELASTIC', 'Elasticsearch'), (2, 'KAFKA', 'Kafka');
	INSERT INTO tier (id, code, libelle, ordre) VALUES (1, 'HOT', 'Hot', 10);
	INSERT INTO zone (id, code, libelle) VALUES (1, 'DC1', 'Zone 1');
	INSERT INTO cluster (id, nom, projet_id, environnement_id, techno_id, tier_id) VALUES
		(1, 'ElasticHot', 1, 1, 1, 1), (2, 'Kafka', 1, 1, 2, NULL);

	INSERT INTO modele (id, type, annee, code, prix_fournisseur_ht, cout_annuel_ht)
		VALUES (1, 'STD', 2020, 'STD-2020', 18000, 4200);
	INSERT INTO revision (id, modele_id, numero, date_effet) VALUES (1, 1, 1, '2020-01-01');
	INSERT INTO composant (revision_id, nature, code, quantite, capacite_unitaire, unite)
		VALUES (1, 'DISQUE_DATA', 'ssd', 12, 8, 'TO');
	INSERT INTO modele (id, type, annee, code, prix_fournisseur_ht, cout_annuel_ht)
		VALUES (2, 'DENSE', 2027, 'DENSE-2027', 42000, 9800);
	INSERT INTO revision (id, modele_id, numero, date_effet) VALUES (2, 2, 1, '2027-01-01'), (3, 2, 2, '2027-03-01');
	INSERT INTO composant (revision_id, nature, code, quantite, capacite_unitaire, unite)
		VALUES (2, 'DISQUE_DATA', 'ssd', 12, 24, 'TO'), (3, 'DISQUE_DATA', 'ssd', 24, 24, 'TO');

	INSERT INTO serveur (id, physical_name, statut, date_entree, zone_id) VALUES (1, 'PHY001', 'EN_SERVICE', '2020-01-01', 1);
	INSERT INTO serveur_revision (serveur_id, revision_id, date_debut) VALUES (1, 1, '2020-01-01');
	INSERT INTO affectation (serveur_id, cluster_id, date_debut) VALUES (1, 1, '2020-01-01');

	INSERT INTO metrique (id, code, libelle, unite) VALUES (1, 'DISQUE_UTILE_TO', 'Disque utile', 'TO');
	INSERT INTO variable (code, libelle, defaut) VALUES ('debit', 'Débit', NULL), ('retention', 'Rétention', NULL);
	INSERT INTO variable_valeur (variable_id, annee, techno_id, valeur, modifie_le)
		VALUES ((SELECT id FROM variable WHERE code='debit'), 2027, 1, 5, '2027-01-01');
	INSERT INTO variable_valeur (variable_id, annee, tier_id, valeur, modifie_le)
		VALUES ((SELECT id FROM variable WHERE code='retention'), 2027, 1, 7, '2027-01-01');
	INSERT INTO regle (nom, metrique_id, expression, niveau_evaluation, composant_offre, techno_id, tier_id, date_creation)
		VALUES ('Disque HOT', 1, 'debit * retention', 'PERIMETRE', 'ssd', 1, 1, '2027-01-01');

	INSERT INTO scenario (id, nom, description, statut, date_creation)
		VALUES (1, 'Renouvellement HOT', 'test', 'ACTIF', '2026-01-01');
	`
	if _, err := d.Base().Exec(script); err != nil {
		t.Fatalf("fixture : %v", err)
	}
}

func TestSyntheseScenarioParHTTP(t *testing.T) {
	serveur, d := serveurDeTest(t)
	client := clientConnecte(t, serveur)
	fixtureSynthese(t, d)

	rep, err := client.Get(serveur.URL + "/scenarios/1/synthese?annee=2027")
	if err != nil {
		t.Fatal(err)
	}
	page := corps(t, rep)
	for _, attendu := range []string{
		"ElasticHot", "Disque HOT", "Kafka", "aucune règle active",
		`href="/clusters/1/dimensionnement?annee=2027&scenario=1"`,
		`href="/scenarios/1/lot?annee=2027"`,
		`href="/comparaison?axe1=cluster&amp;colonne=nb_serveurs&amp;colonne=cout_acquisition&amp;colonne=cout_annuel&amp;colonne=licences_unites&amp;colonne=licences_cout&amp;date=2027-12-31&amp;scenario_a=0&amp;scenario_b=1"`,
		"aucun cluster en déficit sous le scénario",
	} {
		if !strings.Contains(page, attendu) {
			t.Fatalf("synthèse : « %s » attendu : %s", attendu, page)
		}
	}

	// export : mêmes chiffres, avant = après tant que le scénario est vide
	// (besoin 35, offre 96, écart 61 sur ElasticHot ; Kafka sans règle).
	repCSV, err := client.Get(serveur.URL + "/scenarios/1/synthese?annee=2027&export=csv&tableau=synthese")
	if err != nil {
		t.Fatal(err)
	}
	csv := corps(t, repCSV)
	if !strings.HasPrefix(csv, "Cluster;Techno;Tier;Règle limitante;") ||
		!strings.Contains(csv, "ElasticHot;Elasticsearch;Hot;Disque HOT;ssd;35;96;61;35;96;61;1;1;0;0;0;0;") ||
		!strings.Contains(csv, "Kafka;Kafka;;;;;;;;;;0;0;0;0;0;0;aucune règle active") {
		t.Fatalf("export synthèse inattendu : %s", csv)
	}

	// la liste des scénarios mène à la synthèse
	repListe, err := client.Get(serveur.URL + "/scenarios")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(corps(t, repListe), `href="/scenarios/1/synthese"`) {
		t.Fatal("la liste des scénarios doit porter le lien vers la synthèse")
	}
}

func TestComparaisonPregleeDepuisSynthese(t *testing.T) {
	serveur, d := serveurDeTest(t)
	client := clientConnecte(t, serveur)
	fixtureSynthese(t, d)

	rep, err := client.Get(serveur.URL + lienComparaisonScenario(1, 2027))
	if err != nil {
		t.Fatal(err)
	}
	page := corps(t, rep)
	for _, attendu := range []string{
		`class="puce choisi" draggable="true" data-code="cluster">Cluster`,
		`<option value="1" selected>Renouvellement HOT</option>`,
		`class="puce choisi" draggable="true" data-code="nb_serveurs">Nb serveurs`,
		`class="puce choisi" draggable="true" data-code="cout_annuel">`,
		`<input type="hidden" name="colonne" value="cout_annuel">`,
		"Nb serveurs A", "ElasticHot",
	} {
		if !strings.Contains(page, attendu) {
			t.Fatalf("comparaison préréglée : « %s » attendu : %s", attendu, page)
		}
	}
	if strings.Contains(page, `class="puce choisi" draggable="true" data-code="ssd">`) {
		t.Fatal("une colonne hors du préréglage ne doit pas être choisie")
	}

	// sans préréglage : les colonnes par défaut (serveurs, cœurs, RAM,
	// disques, nœuds), pas les coûts, et pas de résultat avant « Générer ».
	repNu, err := client.Get(serveur.URL + "/comparaison")
	if err != nil {
		t.Fatal(err)
	}
	nu := corps(t, repNu)
	for _, code := range []string{"nb_serveurs", "cpu", "ram", "hdd", "ssd", "nb_noeuds"} {
		if !strings.Contains(nu, `<input type="hidden" name="colonne" value="`+code+`">`) {
			t.Fatalf("colonne par défaut « %s » attendue : %s", code, nu)
		}
	}
	if strings.Contains(nu, `class="puce choisi" draggable="true" data-code="cout_annuel">`) || strings.Contains(nu, "Nb serveurs A") {
		t.Fatal("sans préréglage : coûts non choisis et aucun résultat rendu")
	}
}

func TestDimensionnementEnLotParHTTP(t *testing.T) {
	serveur, d := serveurDeTest(t)
	client := clientConnecte(t, serveur)
	fixtureSynthese(t, d)

	repPage, err := client.Get(serveur.URL + "/scenarios/1/lot?annee=2027")
	if err != nil {
		t.Fatal(err)
	}
	page := corps(t, repPage)
	for _, attendu := range []string{
		`name="conserver" value="1" checked`, "STD-2020",
		`<option value="1:2">DENSE-2027</option>`, // modele_tier : tier 1 -> modèle 2
		`<option value="0:2">DENSE-2027</option>`, // sans tier
		`<option value="2:2">DENSE-2027</option>`, // modele_cluster : Kafka -> modèle 2
		"Sans tier",
	} {
		if !strings.Contains(page, attendu) {
			t.Fatalf("page du lot : « %s » attendu : %s", attendu, page)
		}
	}
	jeton := csrfDepuisPage(t, page)

	// aperçu en renouvellement (rien conservé), modèle par tier : ElasticHot
	// -> 1 DENSE-2027 à ajouter, 1 à retirer ; Kafka sans modèle, exclu.
	repApercu, err := client.Get(serveur.URL + "/scenarios/1/lot/apercu?annee=2027&modele_tier=1:2")
	if err != nil {
		t.Fatal(err)
	}
	apercu := corps(t, repApercu)
	for _, attendu := range []string{"DENSE-2027", "rév. 2", "aucun modèle choisi", "Matérialiser le lot",
		`href="/scenarios/1/lot?annee=2027&amp;export=csv&amp;modele_tier=1%3A2&amp;tableau=apercu"`} {
		if !strings.Contains(apercu, attendu) {
			t.Fatalf("aperçu : « %s » attendu : %s", attendu, apercu)
		}
	}
	repCSV, err := client.Get(serveur.URL + "/scenarios/1/lot?annee=2027&modele_tier=1:2&export=csv&tableau=apercu")
	if err != nil {
		t.Fatal(err)
	}
	csv := corps(t, repCSV)
	if !strings.Contains(csv, "ElasticHot;Hot;DENSE-2027;1;;Disque HOT;0;1;42000;9800;\n") ||
		!strings.Contains(csv, "Kafka;;;;;;0;0;;;aucun modèle choisi") {
		t.Fatalf("export aperçu inattendu : %s", csv)
	}

	// le choix par cluster prime sur le tier (tier -> modèle inexistant,
	// cluster -> DENSE-2027) ; en renfort (génération conservée), 0 à ajouter
	// et 0 à retirer : rien à faire, exclu.
	repRenfort, err := client.Get(serveur.URL + "/scenarios/1/lot?annee=2027&modele_tier=1:999&modele_cluster=1:2&conserver=1&export=csv&tableau=apercu")
	if err != nil {
		t.Fatal(err)
	}
	if renfort := corps(t, repRenfort); !strings.Contains(renfort, "ElasticHot;Hot;DENSE-2027;0;;Disque HOT;1;0;0;0;rien à faire") {
		t.Fatalf("renfort : rien à faire attendu : %s", renfort)
	}

	// rien d'actionnable : message, rien d'écrit.
	reqVide := requeteFormulaire(t, http.MethodPost, serveur.URL+"/scenarios/1/lot/materialiser", jeton, url.Values{
		"annee": {"2027"},
	})
	repVide, err := client.Do(reqVide)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(corps(t, repVide), "Rien à matérialiser") {
		t.Fatal("un lot vide doit être refusé avec un message")
	}

	// matérialisation du renouvellement : 1 HYPOTHESE sur ElasticHot, PHY001
	// retiré, redirection htmx vers la synthèse.
	req := requeteFormulaire(t, http.MethodPost, serveur.URL+"/scenarios/1/lot/materialiser", jeton, url.Values{
		"annee": {"2027"}, "modele_tier": {"1:2"},
	})
	req.Header.Set("HX-Request", "true")
	repMat, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if repMat.StatusCode != http.StatusOK || repMat.Header.Get("HX-Redirect") != "/scenarios/1/synthese?annee=2027" {
		t.Fatalf("matérialisation : attendu 200 + HX-Redirect, obtenu %d %q : %s", repMat.StatusCode, repMat.Header.Get("HX-Redirect"), corps(t, repMat))
	}
	sc := int64(1)
	hyp, err := d.ListerServeurs(depot.FiltreServeur{ScenarioID: &sc})
	if err != nil {
		t.Fatal(err)
	}
	var nbHyp int
	for _, s := range hyp {
		if s.Statut == depot.StatutHypothese {
			nbHyp++
			if !strings.HasPrefix(*s.PhysicalName, "HYP-ElasticHot-DENSE-2027-") {
				t.Fatalf("serveur hypothétique inattendu : %+v", s)
			}
		}
	}
	if nbHyp != 1 {
		t.Fatalf("attendu 1 serveur hypothétique, obtenu %d", nbHyp)
	}

	// la synthèse montre l'après : offre 576, écart 541, +1 / −1, coût 42000.
	repSyn, err := client.Get(serveur.URL + "/scenarios/1/synthese?annee=2027&export=csv&tableau=synthese")
	if err != nil {
		t.Fatal(err)
	}
	if syn := corps(t, repSyn); !strings.Contains(syn, "ElasticHot;Elasticsearch;Hot;Disque HOT;ssd;35;96;61;35;576;541;1;1;1;1;42000;9800;") {
		t.Fatalf("synthèse après matérialisation inattendue : %s", syn)
	}
}
