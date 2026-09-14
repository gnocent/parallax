package web

import (
	"strings"
	"testing"
)

// TestVueCapaciteParHTTP : sur la fixture de la synthèse (ElasticHot, règle
// « Disque HOT » 35 To sur ssd, 96 To installés ; Kafka sans règle ni
// serveur), la vue par techno confronte besoin et capa par composant, et
// l'export reprend les mêmes chiffres.
func TestVueCapaciteParHTTP(t *testing.T) {
	serveur, d := serveurDeTest(t)
	client := clientConnecte(t, serveur)
	fixtureSynthese(t, d)

	rep, err := client.Get(serveur.URL + "/capacite?annee=2027&axe1=techno")
	if err != nil {
		t.Fatal(err)
	}
	page := corps(t, rep)
	for _, attendu := range []string{
		"<code>ssd</code>", "Elasticsearch", "Kafka",
		`class="puce choisi" draggable="true" data-code="techno">Techno`,
		`href="/capacite?annee=2027&amp;axe1=techno&amp;export=csv"`,
	} {
		if !strings.Contains(page, attendu) {
			t.Fatalf("vue capacité : « %s » attendu : %s", attendu, page)
		}
	}

	repCSV, err := client.Get(serveur.URL + "/capacite?annee=2027&axe1=techno&axe2=tier&export=csv")
	if err != nil {
		t.Fatal(err)
	}
	csv := corps(t, repCSV)
	if !strings.HasPrefix(csv, "Techno;Tier;Clusters;Serveurs;ssd besoin;ssd capa\n") ||
		!strings.Contains(csv, "Elasticsearch;Hot;1;1;35;96\n") ||
		!strings.Contains(csv, "Kafka;;1;0;0;0\n") ||
		!strings.Contains(csv, "Total;;2;1;35;96\n") {
		t.Fatalf("export capacité inattendu : %s", csv)
	}

	// sans axe explicite : techno × tier par défaut ; avec un axe cluster,
	// une ligne par cluster.
	repDefaut, err := client.Get(serveur.URL + "/capacite?annee=2027&export=csv")
	if err != nil {
		t.Fatal(err)
	}
	if defaut := corps(t, repDefaut); !strings.HasPrefix(defaut, "Techno;Tier;") {
		t.Fatalf("axes par défaut attendus techno × tier : %s", defaut)
	}
	repCluster, err := client.Get(serveur.URL + "/capacite?annee=2027&axe1=cluster&scenario=1&export=csv")
	if err != nil {
		t.Fatal(err)
	}
	if cl := corps(t, repCluster); !strings.Contains(cl, "ElasticHot;1;1;35;96\n") {
		t.Fatalf("vue par cluster sous scénario : %s", cl)
	}

	// projection pluriannuelle : axe année + plage. En 2028 les variables
	// n'ont pas de valeur : besoin 0, capa 96, cluster signalé ; pas de Total.
	repPlage, err := client.Get(serveur.URL + "/capacite?annee=2027&annee_fin=2028&axe1=annee&axe2=techno&export=csv")
	if err != nil {
		t.Fatal(err)
	}
	plage := corps(t, repPlage)
	if !strings.HasPrefix(plage, "Année;Techno;Clusters;Serveurs;ssd besoin;ssd capa\n") ||
		!strings.Contains(plage, "2027;Elasticsearch;1;1;35;96\n") ||
		!strings.Contains(plage, "2028;Elasticsearch;1;1;0;96\n") ||
		strings.Contains(plage, "Total") {
		t.Fatalf("projection pluriannuelle inattendue : %s", plage)
	}
	repPlagePage, err := client.Get(serveur.URL + "/capacite?annee=2027&annee_fin=2028&axe1=annee&axe2=techno")
	if err != nil {
		t.Fatal(err)
	}
	pagePlage := corps(t, repPlagePage)
	for _, attendu := range []string{"2028, ElasticHot", `<td rowspan="2" class="fusion">2027</td>`, "31 décembre de chaque année"} {
		if !strings.Contains(pagePlage, attendu) {
			t.Fatalf("page pluriannuelle : « %s » attendu : %s", attendu, pagePlage)
		}
	}
	// sans l'axe année, la plage est ignorée : une seule année, Total présent.
	repSansAxe, err := client.Get(serveur.URL + "/capacite?annee=2027&annee_fin=2028&axe1=techno&export=csv")
	if err != nil {
		t.Fatal(err)
	}
	if sansAxe := corps(t, repSansAxe); !strings.Contains(sansAxe, "Total;2;1;35;96\n") {
		t.Fatalf("sans axe année : %s", sansAxe)
	}
}

// TestAxeAnneeModeleEtFusion : l'axe « année du modèle » du constructeur de
// vues (génération), et la fusion verticale des cellules d'axes quand
// plusieurs axes sont choisis (vues et comparaison).
func TestAxeAnneeModeleEtFusion(t *testing.T) {
	serveur, d := serveurDeTest(t)
	client := clientConnecte(t, serveur)
	fixtureSynthese(t, d)
	// un second serveur STD-2020 sur ElasticHot, zone distincte : deux lignes
	// sous le même (techno, année du modèle) une fois groupé par zone.
	if _, err := d.Base().Exec(`
		INSERT INTO zone (id, code, libelle) VALUES (2, 'DC2', 'Zone 2');
		INSERT INTO serveur (id, physical_name, statut, date_entree, zone_id) VALUES (2, 'PHY002', 'EN_SERVICE', '2020-01-01', 2);
		INSERT INTO serveur_revision (serveur_id, revision_id, date_debut) VALUES (2, 1, '2020-01-01');
		INSERT INTO affectation (serveur_id, cluster_id, date_debut) VALUES (2, 1, '2020-01-01');
	`); err != nil {
		t.Fatalf("fixture : %v", err)
	}

	rep, err := client.Get(serveur.URL + "/vues/resultat?axe1=techno&axe2=annee_modele&axe3=zone&colonne=nb_serveurs")
	if err != nil {
		t.Fatal(err)
	}
	page := corps(t, rep)
	for _, attendu := range []string{
		"<th>Année du modèle</th>",
		`<td rowspan="2" class="fusion">ELASTIC</td>`,
		`<td rowspan="2" class="fusion">2020</td>`,
		`<td>DC1</td>`,
		`<td>DC2</td>`,
	} {
		if !strings.Contains(page, attendu) {
			t.Fatalf("vues : « %s » attendu : %s", attendu, page)
		}
	}

	repComp, err := client.Get(serveur.URL + "/comparaison/resultat?scenario_a=0&scenario_b=1&axe1=annee_modele&axe2=zone&colonne=nb_serveurs")
	if err != nil {
		t.Fatal(err)
	}
	if comp := corps(t, repComp); !strings.Contains(comp, `<td rowspan="2" class="fusion">2020</td>`) || !strings.Contains(comp, `<td>DC2</td>`) {
		t.Fatalf("comparaison : fusion attendue sur l'année du modèle : %s", comp)
	}
}
