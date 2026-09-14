package web

import (
	"reflect"
	"strings"
	"testing"
)

func TestOrdreTri(t *testing.T) {
	cles := [][]string{{"A", "x"}, {"A", "y"}, {"B", "x"}}
	valeurs := []float64{3, 1, 2}
	valeur := func(i int) float64 { return valeurs[i] }
	// tri dans le groupe parent (A, puis B), jamais à travers
	if got := ordreTri(cles, valeur, false); !reflect.DeepEqual(got, []int{1, 0, 2}) {
		t.Fatalf("asc : %v", got)
	}
	if got := ordreTri(cles, valeur, true); !reflect.DeepEqual(got, []int{0, 1, 2}) {
		t.Fatalf("desc : %v", got)
	}
	// un seul axe : tri complet
	if got := ordreTri([][]string{{"a"}, {"b"}, {"c"}}, valeur, true); !reflect.DeepEqual(got, []int{0, 2, 1}) {
		t.Fatalf("un axe, desc : %v", got)
	}
}

// TestTriEtAxesParHTTP : clic sur un en-tête (vues, comparaison, capacité)
// et quatrième axe de regroupement.
func TestTriEtAxesParHTTP(t *testing.T) {
	serveur, d := serveurDeTest(t)
	client := clientConnecte(t, serveur)
	fixtureSynthese(t, d)
	// deux serveurs de plus en zone DC2 : DC2 = 2 serveurs, DC1 = 1
	if _, err := d.Base().Exec(`
		INSERT INTO zone (id, code, libelle) VALUES (2, 'DC2', 'Zone 2');
		INSERT INTO serveur (id, physical_name, statut, date_entree, zone_id) VALUES
			(2, 'PHY002', 'EN_SERVICE', '2020-01-01', 2), (3, 'PHY003', 'EN_SERVICE', '2020-01-01', 2);
		INSERT INTO serveur_revision (serveur_id, revision_id, date_debut) VALUES (2, 1, '2020-01-01'), (3, 1, '2020-01-01');
		INSERT INTO affectation (serveur_id, cluster_id, date_debut) VALUES (2, 1, '2020-01-01'), (3, 1, '2020-01-01');
	`); err != nil {
		t.Fatalf("fixture : %v", err)
	}
	avant := func(page, a, b string) bool { return strings.Index(page, a) < strings.Index(page, b) }

	// vues : desc met DC2 (2) avant DC1 (1) ; asc l'inverse ; l'en-tête actif
	// et le tri courant rattaché au formulaire
	rep, err := client.Get(serveur.URL + "/vues/resultat?axe1=zone&colonne=nb_serveurs&tri=nb_serveurs&sens=desc")
	if err != nil {
		t.Fatal(err)
	}
	desc := corps(t, rep)
	if !avant(desc, "<td>DC2</td>", "<td>DC1</td>") ||
		!strings.Contains(desc, `class="lien-tri actif"`) || !strings.Contains(desc, "▼") ||
		!strings.Contains(desc, `name="tri" value="nb_serveurs" form="constructeur"`) ||
		!strings.Contains(desc, `hx-get="/vues/resultat?tri=nb_serveurs&sens=asc"`) {
		t.Fatalf("vues, tri desc : %s", desc)
	}
	repAsc, err := client.Get(serveur.URL + "/vues/resultat?axe1=zone&colonne=nb_serveurs&tri=nb_serveurs&sens=asc")
	if err != nil {
		t.Fatal(err)
	}
	if asc := corps(t, repAsc); !avant(asc, "<td>DC1</td>", "<td>DC2</td>") {
		t.Fatalf("vues, tri asc : %s", asc)
	}

	// quatre axes
	rep4, err := client.Get(serveur.URL + "/vues/resultat?axe1=projet&axe2=environnement&axe3=techno&axe4=zone&colonne=nb_serveurs")
	if err != nil {
		t.Fatal(err)
	}
	if p := corps(t, rep4); !strings.Contains(p, "<th>Zone</th>") || !strings.Contains(p, "<td>DC2</td>") {
		t.Fatalf("quatrième axe : %s", p)
	}

	// la page du constructeur porte le sélecteur d'étiquettes, préréglé par l'URL
	repPage, err := client.Get(serveur.URL + "/vues?axe1=techno&axe2=zone")
	if err != nil {
		t.Fatal(err)
	}
	page := corps(t, repPage)
	for _, attendu := range []string{
		`class="axes-selecteur" data-max="6"`,
		`<button type="button" class="puce choisi" draggable="true" data-code="techno">Techno`,
		`<input type="hidden" name="axe1" value="techno">`, `<input type="hidden" name="axe2" value="zone">`,
		`class="puce" draggable="true" data-code="projet">Projet`,
		`/statique/axes.js`,
	} {
		if !strings.Contains(page, attendu) {
			t.Fatalf("sélecteur d'axes : « %s » attendu : %s", attendu, page)
		}
	}

	// comparaison : tri sur la sous-colonne A
	repComp, err := client.Get(serveur.URL + "/comparaison/resultat?axe1=zone&scenario_b=1&colonne=nb_serveurs&tri=nb_serveurs:a&sens=desc")
	if err != nil {
		t.Fatal(err)
	}
	if comp := corps(t, repComp); !avant(comp, "<td>DC2</td>", "<td>DC1</td>") || !strings.Contains(comp, `hx-get="/comparaison/resultat?tri=nb_serveurs:a&sens=asc"`) {
		t.Fatalf("comparaison, tri desc : %s", comp)
	}

	// capacité : navigation GET, l'en-tête porte l'URL du tri suivant
	repCapa, err := client.Get(serveur.URL + "/capacite?annee=2027&axe1=techno&tri=ssd:capa&sens=asc")
	if err != nil {
		t.Fatal(err)
	}
	capa := corps(t, repCapa)
	if !avant(capa, "<td>Kafka</td>", "<td>Elasticsearch</td>") ||
		!strings.Contains(capa, `href="/capacite?annee=2027&amp;axe1=techno&amp;sens=desc&amp;tri=ssd%3Acapa"`) {
		t.Fatalf("capacité, tri asc : %s", capa)
	}
	repCapaCSV, err := client.Get(serveur.URL + "/capacite?annee=2027&axe1=techno&tri=ssd:capa&sens=desc&export=csv")
	if err != nil {
		t.Fatal(err)
	}
	if csv := corps(t, repCapaCSV); !avant(csv, "Elasticsearch;", "Kafka;") {
		t.Fatalf("capacité, export trié desc : %s", csv)
	}
}
