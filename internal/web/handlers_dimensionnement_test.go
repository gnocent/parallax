package web

import (
	"net/http"
	"strings"
	"testing"
)

// TestBesoinOffreClusterHOT reproduit le scénario de référence du moteur de
// capacité (spec/vectors.json, cas "HOT 2027") au niveau écran : une règle
// disque et une règle CPU sur un cluster HOT, un débit et une rétention
// déclarés, un parc réel installé, et vérifie que le besoin, l'offre et la
// règle limitante s'affichent correctement.
func TestBesoinOffreCluster(t *testing.T) {
	serveur, d := serveurDeTest(t)
	client := clientConnecte(t, serveur)
	base := d.Base()

	script := `
	INSERT INTO projet (id, code, libelle) VALUES (1, 'LOGS', 'Log Management');
	INSERT INTO environnement (id, code, libelle, ordre) VALUES (1, 'PROD', 'Production', 10);
	INSERT INTO techno (id, code, libelle) VALUES (1, 'ELASTIC', 'Elasticsearch');
	INSERT INTO tier (id, code, libelle, ordre) VALUES (1, 'HOT', 'Hot', 10);
	INSERT INTO cluster (id, nom, projet_id, environnement_id, techno_id, tier_id) VALUES
		(1, 'ElasticHot', 1, 1, 1, 1);

	INSERT INTO modele (id, type, annee, code) VALUES (1, 'STD', 2020, 'STD-2020');
	INSERT INTO revision (id, modele_id, numero, date_effet) VALUES (1, 1, 1, '2020-01-01');
	INSERT INTO composant (revision_id, nature, code, quantite, capacite_unitaire, unite) VALUES
		(1, 'DISQUE_DATA', 'ssd', 12, 8, 'TO');

	INSERT INTO serveur (id, physical_name, statut, date_entree) VALUES (1, 'PHY001', 'EN_SERVICE', '2020-01-01');
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
	`
	if _, err := base.Exec(script); err != nil {
		t.Fatalf("fixture : %v", err)
	}

	rep, err := client.Get(serveur.URL + "/clusters/1/besoin-offre?annee=2027")
	if err != nil {
		t.Fatal(err)
	}
	page := corps(t, rep)

	// offre installée : 1 serveur x 12 x 8 To = 96 To ; besoin = 5 x 7 = 35 To
	if !strings.Contains(page, "Disque HOT") {
		t.Fatalf("la règle devrait apparaître : %s", page)
	}
	if !strings.Contains(page, "35") {
		t.Fatalf("besoin attendu 35 (5x7) : %s", page)
	}
	if !strings.Contains(page, "96") {
		t.Fatalf("offre attendue 96 (12x8) : %s", page)
	}
	if !strings.Contains(page, "limitante") {
		t.Fatalf("l'unique règle applicable doit être marquée limitante : %s", page)
	}
}

// TestBesoinOffreClusterSousScenario prolonge la fixture précédente avec un
// scénario qui retire PHY001 (sans le réaffecter), ajoute un serveur
// hypothétique de capacité différente, et surcharge la variable de débit au
// niveau cluster — les trois leviers de la v2.1 (retrait, ajout, surcharge de
// variable). Vérifie que le réel reste inchangé et que le delta identifie
// l'arrivée et le départ.
func TestBesoinOffreClusterSousScenario(t *testing.T) {
	serveur, d := serveurDeTest(t)
	client := clientConnecte(t, serveur)
	base := d.Base()

	script := `
	INSERT INTO projet (id, code, libelle) VALUES (1, 'LOGS', 'Log Management');
	INSERT INTO environnement (id, code, libelle, ordre) VALUES (1, 'PROD', 'Production', 10);
	INSERT INTO techno (id, code, libelle) VALUES (1, 'ELASTIC', 'Elasticsearch');
	INSERT INTO tier (id, code, libelle, ordre) VALUES (1, 'HOT', 'Hot', 10);
	INSERT INTO cluster (id, nom, projet_id, environnement_id, techno_id, tier_id) VALUES
		(1, 'ElasticHot', 1, 1, 1, 1);

	INSERT INTO modele (id, type, annee, code) VALUES (1, 'STD', 2020, 'STD-2020');
	INSERT INTO revision (id, modele_id, numero, date_effet) VALUES
		(1, 1, 1, '2020-01-01'), (2, 1, 2, '2025-01-01');
	INSERT INTO composant (revision_id, nature, code, quantite, capacite_unitaire, unite) VALUES
		(1, 'DISQUE_DATA', 'ssd', 12, 8, 'TO'),
		(2, 'DISQUE_DATA', 'ssd', 20, 8, 'TO');

	INSERT INTO serveur (id, physical_name, statut, date_entree) VALUES (1, 'PHY001', 'EN_SERVICE', '2020-01-01');
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
		VALUES (1, 'Renfort HOT', 'test', 'ACTIF', '2026-01-01');

	-- retrait de PHY001 dans le scénario, sans réaffectation (ligne ouverte
	-- puis refermée dans le seau du scénario — voir modele-donnees.md §6).
	-- L'hypothèse est datée DANS l'année de planification (2027) : l'offre se
	-- lit au 31/12 de cette année (dateReferenceOffre), pas à la date du jour.
	INSERT INTO affectation (serveur_id, cluster_id, date_debut, date_fin, scenario_id)
		VALUES (1, 1, '2027-01-01', '2027-02-01', 1);

	-- ajout hypothétique, sur la révision 2 (20x8 = 160 To), lui aussi en 2027.
	INSERT INTO serveur (id, physical_name, statut, scenario_id, date_entree)
		VALUES (2, 'PHY-HYP', 'HYPOTHESE', 1, '2027-01-01');
	INSERT INTO serveur_revision (serveur_id, revision_id, date_debut) VALUES (2, 2, '2027-01-01');
	INSERT INTO affectation (serveur_id, cluster_id, date_debut, scenario_id) VALUES (2, 1, '2027-01-01', 1);

	-- surcharge du débit au niveau cluster, dans le scénario.
	INSERT INTO variable_valeur (variable_id, scenario_id, annee, cluster_id, valeur, modifie_le)
		VALUES ((SELECT id FROM variable WHERE code='debit'), 1, 2027, 1, 10, '2027-01-01');
	`
	if _, err := base.Exec(script); err != nil {
		t.Fatalf("fixture : %v", err)
	}

	// réel : inchangé, comme TestBesoinOffreCluster (35 de besoin, 96 d'offre).
	repReel, err := client.Get(serveur.URL + "/clusters/1/besoin-offre?annee=2027")
	if err != nil {
		t.Fatal(err)
	}
	pageReel := corps(t, repReel)
	if !strings.Contains(pageReel, "96") || strings.Contains(pageReel, "160") {
		t.Fatalf("le réel doit rester à l'offre installée (96), sans trace du scénario : %s", pageReel)
	}

	// sous le scénario : débit surchargé à 10 -> besoin 70 ; offre 160 (PHY-HYP
	// seul, PHY001 retiré) ; écart 90.
	repSc, err := client.Get(serveur.URL + "/clusters/1/besoin-offre?annee=2027&scenario=1")
	if err != nil {
		t.Fatal(err)
	}
	pageSc := corps(t, repSc)
	if !strings.Contains(pageSc, "70") {
		t.Fatalf("besoin sous scénario attendu 70 (10x7, débit surchargé) : %s", pageSc)
	}
	if !strings.Contains(pageSc, "160") {
		t.Fatalf("offre sous scénario attendue 160 (PHY-HYP seul, PHY001 retiré) : %s", pageSc)
	}
	if !strings.Contains(pageSc, "PHY-HYP") || !strings.Contains(pageSc, "PHY001") {
		t.Fatalf("le delta doit citer l'arrivée de PHY-HYP et le départ de PHY001 : %s", pageSc)
	}

	// en 2026, avant l'hypothèse, le scénario ne change encore rien à l'offre
	// (PHY001 encore là, PHY-HYP pas encore) ; et comme aucune variable n'a de
	// valeur pour 2026, le besoin est incalculable : message sur la page,
	// jamais une 500.
	rep2026, err := client.Get(serveur.URL + "/clusters/1/besoin-offre?annee=2026&scenario=1")
	if err != nil {
		t.Fatal(err)
	}
	if rep2026.StatusCode != http.StatusOK {
		t.Fatalf("année sans valeurs : attendu 200 avec message, obtenu %d", rep2026.StatusCode)
	}
	page2026 := corps(t, rep2026)
	if !strings.Contains(page2026, "Calcul impossible") || !strings.Contains(page2026, "non résolue") {
		t.Fatalf("une variable sans valeur pour l'année doit être expliquée sur la page : %s", page2026)
	}
	if strings.Contains(page2026, "PHY-HYP") {
		t.Fatalf("en 2026 l'ajout hypothétique de 2027 ne doit pas encore apparaître dans le delta : %s", page2026)
	}
}
