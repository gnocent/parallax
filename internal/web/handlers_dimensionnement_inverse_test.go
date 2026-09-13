package web

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"parallax/internal/depot"
)

// TestDimensionnementInverseParHTTP couvre la v2.3 de bout en bout : la page,
// le tableau comparatif en renouvellement complet puis en renfort (génération
// conservée), et la matérialisation dans un scénario — visible ensuite dans
// le delta du besoin/offre.
func TestDimensionnementInverseParHTTP(t *testing.T) {
	serveur, d := serveurDeTest(t)
	client := clientConnecte(t, serveur)
	base := d.Base()

	script := `
	INSERT INTO projet (id, code, libelle) VALUES (1, 'LOGS', 'Log Management');
	INSERT INTO environnement (id, code, libelle, ordre) VALUES (1, 'PROD', 'Production', 10);
	INSERT INTO techno (id, code, libelle) VALUES (1, 'ELASTIC', 'Elasticsearch');
	INSERT INTO tier (id, code, libelle, ordre) VALUES (1, 'HOT', 'Hot', 10);
	INSERT INTO zone (id, code, libelle) VALUES (1, 'DC1', 'Zone 1');
	INSERT INTO cluster (id, nom, projet_id, environnement_id, techno_id, tier_id) VALUES
		(1, 'ElasticHot', 1, 1, 1, 1);

	-- génération installée : STD-2020, 12x8 = 96 To
	INSERT INTO modele (id, type, annee, code, prix_fournisseur_ht, cout_annuel_ht)
		VALUES (1, 'STD', 2020, 'STD-2020', 18000, 4200);
	INSERT INTO revision (id, modele_id, numero, date_effet) VALUES (1, 1, 1, '2020-01-01');
	INSERT INTO composant (revision_id, nature, code, quantite, capacite_unitaire, unite)
		VALUES (1, 'DISQUE_DATA', 'ssd', 12, 8, 'TO');
	-- candidat : DENSE-2027, deux révisions, la dernière fait 24x24 = 576 To
	INSERT INTO modele (id, type, annee, code, prix_fournisseur_ht, cout_annuel_ht)
		VALUES (2, 'DENSE', 2027, 'DENSE-2027', 42000, 9800);
	INSERT INTO revision (id, modele_id, numero, date_effet) VALUES (2, 2, 1, '2027-01-01'), (3, 2, 2, '2027-03-01');
	INSERT INTO composant (revision_id, nature, code, quantite, capacite_unitaire, unite)
		VALUES (2, 'DISQUE_DATA', 'ssd', 12, 24, 'TO'), (3, 'DISQUE_DATA', 'ssd', 24, 24, 'TO');

	INSERT INTO serveur (id, physical_name, statut, date_entree, zone_id) VALUES (1, 'PHY001', 'EN_SERVICE', '2020-01-01', 1);
	INSERT INTO serveur_revision (serveur_id, revision_id, date_debut) VALUES (1, 1, '2020-01-01');
	INSERT INTO affectation (serveur_id, cluster_id, date_debut) VALUES (1, 1, '2020-01-01');

	-- besoin 2027 : 5 x 7 = 35 To
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
	if _, err := base.Exec(script); err != nil {
		t.Fatalf("fixture : %v", err)
	}

	// la page liste la génération installée et le candidat.
	repPage, err := client.Get(serveur.URL + "/clusters/1/dimensionnement?annee=2027&scenario=1")
	if err != nil {
		t.Fatal(err)
	}
	page := corps(t, repPage)
	if !strings.Contains(page, "STD-2020") || !strings.Contains(page, "DENSE-2027") {
		t.Fatalf("la page doit montrer la génération installée et le candidat : %s", page)
	}
	jeton := csrfDepuisPage(t, page)

	// renouvellement complet (rien conservé) : 35 To / 576 -> 1 serveur, sur
	// la dernière révision (n°2), surplus 576 - 35 = 541.
	repRes, err := client.Get(serveur.URL + "/clusters/1/dimensionnement/resultat?annee=2027&scenario=1&candidat=2")
	if err != nil {
		t.Fatal(err)
	}
	res := corps(t, repRes)
	for _, attendu := range []string{"rév. 2", "<strong>1</strong>", "541", "42000", "9800", "1 à retirer", "Matérialiser"} {
		if !strings.Contains(res, attendu) {
			t.Fatalf("résultat renouvellement : « %s » attendu : %s", attendu, res)
		}
	}

	// renfort en conservant la génération STD : 96 To conservés couvrent les
	// 35 To -> 0 serveur à ajouter, rien à retirer.
	repRenfort, err := client.Get(serveur.URL + "/clusters/1/dimensionnement/resultat?annee=2027&scenario=1&candidat=2&conserver=1")
	if err != nil {
		t.Fatal(err)
	}
	renfort := corps(t, repRenfort)
	if !strings.Contains(renfort, "<strong>0</strong>") || !strings.Contains(renfort, "0 à retirer") {
		t.Fatalf("résultat renfort : 0 à ajouter et 0 à retirer attendus : %s", renfort)
	}

	// matérialisation du renouvellement : 1 serveur HYPOTHESE créé, PHY001
	// retiré, redirection htmx vers le besoin/offre du scénario.
	req := requeteFormulaire(t, http.MethodPost, serveur.URL+"/clusters/1/dimensionnement/materialiser", jeton, url.Values{
		"annee": {"2027"}, "scenario": {"1"}, "candidat": {"2"}, "modele_id": {"2"},
	})
	req.Header.Set("HX-Request", "true")
	repMat, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if repMat.StatusCode != http.StatusOK || !strings.Contains(repMat.Header.Get("HX-Redirect"), "/clusters/1/besoin-offre?annee=2027&scenario=1") {
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
			if !strings.HasPrefix(*s.PhysicalName, "HYP-ElasticHot-DENSE-2027-") || s.ZoneID == nil || *s.ZoneID != 1 {
				t.Fatalf("serveur hypothétique inattendu : %+v", s)
			}
		}
	}
	if nbHyp != 1 {
		t.Fatalf("attendu 1 serveur hypothétique, obtenu %d : %+v", nbHyp, hyp)
	}

	repBO, err := client.Get(serveur.URL + "/clusters/1/besoin-offre?annee=2027&scenario=1")
	if err != nil {
		t.Fatal(err)
	}
	bo := corps(t, repBO)
	if !strings.Contains(bo, "576") || !strings.Contains(bo, "PHY001") || !strings.Contains(bo, "retiré, sans réaffectation") {
		t.Fatalf("le besoin/offre sous scénario doit montrer l'offre 576 et le départ de PHY001 : %s", bo)
	}

	// sans scénario, pas de matérialisation : message, rien d'écrit.
	reqReel := requeteFormulaire(t, http.MethodPost, serveur.URL+"/clusters/1/dimensionnement/materialiser", jeton, url.Values{
		"annee": {"2027"}, "scenario": {"0"}, "candidat": {"2"}, "modele_id": {"2"},
	})
	repReel, err := client.Do(reqReel)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(corps(t, repReel), "Choisissez un scénario") {
		t.Fatal("matérialiser sans scénario doit être refusé avec un message")
	}
}
