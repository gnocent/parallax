package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"parallax/internal/auth"
	"parallax/internal/db"
	"parallax/internal/depot"
)

// serveurLicencesDeTest construit son propre *serveur (comme
// serveurImportModelesDeTest) : routesLicences n'est pas encore appelée
// depuis routes() (server.go, jamais modifié par ce lot). Les routes du
// constructeur de vues sont câblées aussi, pour vérifier que les contrats
// saisis ici alimentent bien les colonnes de licences d'une vue.
func serveurLicencesDeTest(t *testing.T) (*httptest.Server, *depot.Depot) {
	t.Helper()
	chemin := filepath.Join(t.TempDir(), "web-test-licences.db")
	base, err := db.Ouvrir(chemin)
	if err != nil {
		t.Fatalf("ouverture de la base de test : %v", err)
	}
	t.Cleanup(func() { _ = base.Close() })

	d := depot.Nouveau(base)
	hash, err := auth.HacherMotDePasse("s3cret!")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.CreerUtilisateur(depot.Utilisateur{
		Login: "gnocent", Hash: hash, Role: depot.RoleEditeur,
	}); err != nil {
		t.Fatal(err)
	}

	svc := auth.NouveauService(d)
	s := &serveur{mux: http.NewServeMux(), gabarits: chargerGabarits(), depot: d, auth: svc}
	s.mux.HandleFunc("GET /connexion", s.connexionFormulaire)
	s.mux.HandleFunc("POST /connexion", s.connexionSoumettre)
	s.mux.HandleFunc("POST /deconnexion", s.exigerAuth(s.verifierCSRF(s.deconnexionSoumettre)))
	s.routesLicences()
	s.routesVues()

	handler := recuperer(journaliser(s.chargerSession(s.mux)))
	serveurHTTP := httptest.NewServer(handler)
	t.Cleanup(serveurHTTP.Close)
	return serveurHTTP, d
}

// fixtureLicences : un cluster ELASTIC et un cluster KAFKA sur le même
// projet, un serveur chacun, révision à 768 Go de RAM et 4 nœuds pour la
// techno du cluster (modele_noeud pour les deux technos).
func fixtureLicences(t *testing.T, d *depot.Depot) {
	t.Helper()
	script := `
	INSERT INTO projet (id, code, libelle) VALUES (1, 'LOGS', 'Log Management');
	INSERT INTO environnement (id, code, libelle, ordre) VALUES (1, 'PROD', 'Production', 10);
	INSERT INTO techno (id, code, libelle) VALUES (1, 'ELASTIC', 'Elasticsearch'), (2, 'KAFKA', 'Kafka');
	INSERT INTO cluster (id, nom, projet_id, environnement_id, techno_id) VALUES
		(1, 'ElasticHot', 1, 1, 1), (2, 'Kafka', 1, 1, 2);
	INSERT INTO modele (id, type, annee, code) VALUES (1, 'STD', 2020, 'STD-2020');
	INSERT INTO revision (id, modele_id, numero, date_effet) VALUES (1, 1, 1, '2020-01-01');
	INSERT INTO composant (revision_id, nature, code, quantite, capacite_unitaire, unite)
		VALUES (1, 'RAM', 'ram', 1, 768, 'GO');
	INSERT INTO modele_noeud (revision_id, techno_id, nb_noeuds) VALUES (1, 1, 4), (1, 2, 4);
	INSERT INTO serveur (id, physical_name, statut, date_entree) VALUES
		(1, 'PHY001', 'EN_SERVICE', '2020-01-01'), (2, 'PHY002', 'EN_SERVICE', '2020-01-01');
	INSERT INTO serveur_revision (serveur_id, revision_id, date_debut) VALUES (1, 1, '2020-01-01'), (2, 1, '2020-01-01');
	INSERT INTO affectation (serveur_id, cluster_id, date_debut) VALUES (1, 1, '2020-01-01'), (2, 2, '2020-01-01');
	`
	if _, err := d.Base().Exec(script); err != nil {
		t.Fatalf("fixture : %v", err)
	}
}

func requeteLicences(t *testing.T, client *http.Client, methode, url, jeton string, form url.Values) *http.Response {
	t.Helper()
	req, err := http.NewRequest(methode, url, strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-CSRF-Token", jeton)
	rep, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return rep
}

// TestLicencesEcranPuisVue : un contrat créé par l'écran, puis une vue avec
// la colonne « Unités de licence » sans axe techno — le tableau se découpe
// par techno (axe ajouté) et porte la bonne valeur ; l'export CSV la reprend.
func TestLicencesEcranPuisVue(t *testing.T) {
	serveur, d := serveurLicencesDeTest(t)
	client := clientConnecte(t, serveur)
	fixtureLicences(t, d)
	jeton := jetonCSRFDepuisPage(t, client, serveur, "/licences")

	// page vide : formulaire, sélecteur de scénario, export
	repPage, err := client.Get(serveur.URL + "/licences")
	if err != nil {
		t.Fatal(err)
	}
	page := corps(t, repPage)
	if !strings.Contains(page, `name="scenario"`) || !strings.Contains(page, "Aucun contrat de licence") {
		t.Fatalf("la page devrait proposer le sélecteur de scénario et un tableau vide : %s", page)
	}
	if !strings.Contains(page, `href="/licences?export=csv"`) {
		t.Fatalf("les boutons d'export universel devraient viser /licences : %s", page)
	}

	// création : RAM par machine, 256 Go, 1000 HT l'unité → PHY001 : ceil(768/256) = 3
	repCreer := requeteLicences(t, client, http.MethodPost, serveur.URL+"/licences", jeton, url.Values{
		"scenario": {"0"}, "techno_id": {"1"}, "annee": {"2025"}, "mecanisme": {"RAM"}, "niveau": {"MACHINE"},
		"ram_max_go": {"256"}, "cout_unitaire_ht": {"1000"}, "commentaire": {"contrat 2025"},
	})
	fragment := corps(t, repCreer)
	if repCreer.StatusCode != http.StatusOK || !strings.Contains(fragment, "ELASTIC") || !strings.Contains(fragment, "contrat 2025") {
		t.Fatalf("création : attendu le tableau avec le contrat, obtenu %d : %s", repCreer.StatusCode, fragment)
	}
	if strings.Contains(fragment, "message-erreur") {
		t.Fatalf("création : aucune erreur attendue : %s", fragment)
	}

	// doublon : message métier dans le tableau, pas de 5xx
	repDoublon := requeteLicences(t, client, http.MethodPost, serveur.URL+"/licences", jeton, url.Values{
		"techno_id": {"1"}, "annee": {"2025"}, "mecanisme": {"NOEUDS"}, "niveau": {"MACHINE"},
	})
	if doublon := corps(t, repDoublon); repDoublon.StatusCode != http.StatusOK || !strings.Contains(doublon, "existe déjà") {
		t.Fatalf("doublon : attendu 200 avec message, obtenu %d : %s", repDoublon.StatusCode, doublon)
	}

	// validation : RAM max absente pour RAM → message, pas de 5xx
	repInvalide := requeteLicences(t, client, http.MethodPost, serveur.URL+"/licences", jeton, url.Values{
		"techno_id": {"2"}, "annee": {"2025"}, "mecanisme": {"RAM"}, "niveau": {"GLOBAL"},
	})
	if invalide := corps(t, repInvalide); repInvalide.StatusCode != http.StatusOK || !strings.Contains(invalide, "RAM max") {
		t.Fatalf("validation : attendu 200 avec message, obtenu %d : %s", repInvalide.StatusCode, invalide)
	}

	contrats, err := d.ListerLicenceContrats(nil)
	if err != nil || len(contrats) != 1 {
		t.Fatalf("un seul contrat attendu en base : %v %+v", err, contrats)
	}
	id := contrats[0].ID

	// vue sans axe techno, colonne licences : l'axe Techno est ajouté, ELASTIC = 3 unités / 3000 HT, KAFKA (sans contrat) = 0
	repVue, err := client.Get(serveur.URL + "/vues/resultat?date=2026-03-01&colonne=nb_serveurs&colonne=licences_unites&colonne=licences_cout")
	if err != nil {
		t.Fatal(err)
	}
	vue := corps(t, repVue)
	if repVue.StatusCode != http.StatusOK {
		t.Fatalf("vue : attendu 200, obtenu %d : %s", repVue.StatusCode, vue)
	}
	if !strings.Contains(vue, "<th>Techno</th>") || !strings.Contains(vue, "Unités de licence") || !strings.Contains(vue, "Coût licences (HT)") {
		t.Fatalf("la vue devrait porter l'axe Techno ajouté et les colonnes de licences : %s", vue)
	}
	iElastic := strings.Index(vue, "ELASTIC")
	iKafka := strings.Index(vue, "KAFKA")
	if iElastic < 0 || iKafka < 0 || iElastic > iKafka {
		t.Fatalf("les deux technos devraient apparaître, ELASTIC d'abord : %s", vue)
	}
	ligneElastic := vue[iElastic:iKafka]
	if !strings.Contains(ligneElastic, "<td>1</td><td>3</td><td>3000</td>") {
		t.Fatalf("ELASTIC : attendu 1 serveur, 3 unités, 3000 HT : %s", ligneElastic)
	}
	ligneKafka := vue[iKafka:]
	if !strings.Contains(ligneKafka, "<td>1</td><td>0</td><td>0</td>") {
		t.Fatalf("KAFKA sans contrat : attendu 1 serveur, 0 unité, 0 HT : %s", ligneKafka)
	}
	if strings.Contains(vue, "Niveau global") {
		t.Fatalf("pas de note GLOBAL pour un contrat MACHINE : %s", vue)
	}

	// avant le contrat (2024) : aucun contrat en vigueur, 0 partout
	repAvant, _ := client.Get(serveur.URL + "/vues/resultat?date=2024-06-01&colonne=licences_unites")
	if avant := sansBlancsEntreBalises(corps(t, repAvant)); !strings.Contains(avant, "<td>ELASTIC</td><td>0</td>") {
		t.Fatalf("avant l'année du contrat, les unités devraient être à 0 : %s", avant)
	}

	// export CSV de la vue avec la colonne
	repCSV, err := client.Get(serveur.URL + "/vues/resultat.csv?date=2026-03-01&colonne=licences_unites")
	if err != nil {
		t.Fatal(err)
	}
	csvCorps := corps(t, repCSV)
	if !strings.HasPrefix(csvCorps, "Techno;Unités de licence\n") || !strings.Contains(csvCorps, "ELASTIC;3\n") || !strings.Contains(csvCorps, "KAFKA;0\n") {
		t.Fatalf("export CSV inattendu : %q", csvCorps)
	}

	// passage en GLOBAL par l'édition en ligne : la note apparaît sous la vue
	repEdit, _ := client.Get(serveur.URL + "/licences/" + idTexte(id) + "/editer")
	if edition := corps(t, repEdit); !strings.Contains(edition, `name="ram_max_go" value="256"`) {
		t.Fatalf("le formulaire d'édition devrait pré-remplir la RAM max : %s", edition)
	}
	repModif := requeteLicences(t, client, http.MethodPut, serveur.URL+"/licences/"+idTexte(id), jeton, url.Values{
		"techno_id": {"1"}, "annee": {"2025"}, "mecanisme": {"RAM"}, "niveau": {"GLOBAL"}, "ram_max_go": {"256"},
		"cout_unitaire_ht": {"1000"}, "commentaire": {"contrat 2025"}, // la ligne d'édition renvoie tous ses champs (hx-include="closest tr")
	})
	if modif := corps(t, repModif); repModif.StatusCode != http.StatusOK || !strings.Contains(modif, "<td>GLOBAL</td>") {
		t.Fatalf("modification : attendu la ligne en lecture au niveau GLOBAL, obtenu %d : %s", repModif.StatusCode, modif)
	}
	repVueGlobal, _ := client.Get(serveur.URL + "/vues/resultat?date=2026-03-01&axe1=projet&colonne=licences_unites")
	vueGlobal := corps(t, repVueGlobal)
	if !strings.Contains(vueGlobal, "Niveau global") || !strings.Contains(vueGlobal, "<th>Projet</th><th>Techno</th>") {
		t.Fatalf("un contrat GLOBAL affiché devrait déclencher la note et l'axe techno en fin : %s", vueGlobal)
	}

	// modification invalide : le formulaire d'édition revient avec l'erreur
	repModifInvalide := requeteLicences(t, client, http.MethodPut, serveur.URL+"/licences/"+idTexte(id), jeton, url.Values{
		"techno_id": {"1"}, "annee": {"2025"}, "mecanisme": {"RAM"}, "niveau": {"GLOBAL"}, "ram_max_go": {""},
	})
	if modif := corps(t, repModifInvalide); !strings.Contains(modif, "message-erreur") || !strings.Contains(modif, `name="niveau"`) {
		t.Fatalf("modification invalide : attendu le formulaire d'édition avec l'erreur : %s", modif)
	}

	// export universel de l'écran
	repExport, _ := client.Get(serveur.URL + "/licences?export=csv")
	if export := corps(t, repExport); !strings.HasPrefix(export, "Techno;Année;Mécanisme;Niveau;RAM max (Go);Coût unitaire (HT);Commentaire;Seau\n") ||
		!strings.Contains(export, "ELASTIC;2025;RAM;GLOBAL;256;1000;contrat 2025;réel") {
		t.Fatalf("export CSV des licences inattendu : %q", export)
	}

	// suppression : le tableau revient vide
	repSuppr := requeteLicences(t, client, http.MethodPost, serveur.URL+"/licences/"+idTexte(id)+"/supprimer?scenario=0", jeton, nil)
	if suppr := corps(t, repSuppr); repSuppr.StatusCode != http.StatusOK || !strings.Contains(suppr, "Aucun contrat de licence") {
		t.Fatalf("suppression : attendu le tableau vide, obtenu %d : %s", repSuppr.StatusCode, suppr)
	}
}

// TestLicencesSurchargeParScenario : un contrat du réel et sa surcharge dans
// un scénario ; la page sous scénario montre les deux avec leur badge, et la
// vue sous scénario compte avec la surcharge.
func TestLicencesSurchargeParScenario(t *testing.T) {
	serveur, d := serveurLicencesDeTest(t)
	client := clientConnecte(t, serveur)
	fixtureLicences(t, d)
	if _, err := d.Base().Exec(`INSERT INTO scenario (id, nom, description, statut, date_creation)
		VALUES (1, 'Renégociation', 'test', 'ACTIF', '2026-01-01')`); err != nil {
		t.Fatal(err)
	}
	jeton := jetonCSRFDepuisPage(t, client, serveur, "/licences")

	// réel : NOEUDS → PHY001 = 4 unités ; scénario : RAM 128 Go → ceil(768/128) = 6
	corps(t, requeteLicences(t, client, http.MethodPost, serveur.URL+"/licences", jeton, url.Values{
		"scenario": {"0"}, "techno_id": {"1"}, "annee": {"2025"}, "mecanisme": {"NOEUDS"}, "niveau": {"MACHINE"},
	}))
	repSc := requeteLicences(t, client, http.MethodPost, serveur.URL+"/licences", jeton, url.Values{
		"scenario": {"1"}, "techno_id": {"1"}, "annee": {"2025"}, "mecanisme": {"RAM"}, "niveau": {"MACHINE"}, "ram_max_go": {"128"},
	})
	if fragment := corps(t, repSc); !strings.Contains(fragment, `<span class="badge">scénario</span>`) || !strings.Contains(fragment, `<span class="badge badge-actif">réel</span>`) {
		t.Fatalf("sous scénario, le tableau devrait montrer le réel et la surcharge : %s", fragment)
	}

	repReel, _ := client.Get(serveur.URL + "/licences")
	if page := corps(t, repReel); strings.Contains(page, `<span class="badge">scénario</span>`) {
		t.Fatalf("la page du réel ne doit pas montrer la surcharge du scénario : %s", page)
	}

	repVueReel, _ := client.Get(serveur.URL + "/vues/resultat?date=2026-01-01&axe1=techno&colonne=licences_unites")
	if vue := sansBlancsEntreBalises(corps(t, repVueReel)); !strings.Contains(vue, "<td>ELASTIC</td><td>4</td>") {
		t.Fatalf("réel : attendu 4 unités (nœuds) : %s", vue)
	}
	repVueSc, _ := client.Get(serveur.URL + "/vues/resultat?date=2026-01-01&scenario=1&axe1=techno&colonne=licences_unites")
	if vue := sansBlancsEntreBalises(corps(t, repVueSc)); !strings.Contains(vue, "<td>ELASTIC</td><td>6</td>") {
		t.Fatalf("scénario : attendu 6 unités (RAM 128 Go) : %s", vue)
	}
}

func idTexte(id int64) string { return strconv.FormatInt(id, 10) }

// sansBlancsEntreBalises efface les blancs entre deux balises : le gabarit
// vues_resultat met les cellules d'axes et de valeurs sur des lignes
// distinctes, on veut comparer « <td>ELASTIC</td><td>4</td> » sans se
// soucier de l'indentation.
func sansBlancsEntreBalises(html string) string {
	return regexp.MustCompile(`>\s+<`).ReplaceAllString(html, "><")
}
