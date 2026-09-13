package web

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"parallax/internal/depot"
)

// seedImportServeursFixtures sème une zone, un projet, un environnement,
// une techno, un cluster, un modèle et sa première révision — de quoi
// résoudre les trois familles de références (zone_code,
// projet_code/environnement_code/cluster_nom, modele_code) d'une ligne de
// CSV complète.
func seedImportServeursFixtures(t *testing.T, d *depot.Depot) (dc depot.Zone, cluster depot.Cluster, revision depot.Revision) {
	t.Helper()
	dc, err := d.CreerZone(depot.Zone{Code: "DC1", Libelle: "Zone 1"})
	if err != nil {
		t.Fatalf("seed zone : %v", err)
	}
	projet, err := d.CreerProjet(depot.Projet{Code: "LOGS", Libelle: "Log Management"})
	if err != nil {
		t.Fatalf("seed projet : %v", err)
	}
	env, err := d.CreerEnvironnement(depot.Environnement{Code: "PROD", Libelle: "Production", Ordre: 1})
	if err != nil {
		t.Fatalf("seed environnement : %v", err)
	}
	techno, err := d.CreerTechno(depot.Techno{Code: "ELASTIC", Libelle: "Elasticsearch"})
	if err != nil {
		t.Fatalf("seed techno : %v", err)
	}
	cluster, err = d.CreerCluster(depot.Cluster{
		Nom: "ElasticHot1", ProjetID: projet.ID, EnvironnementID: env.ID, TechnoID: techno.ID,
	})
	if err != nil {
		t.Fatalf("seed cluster : %v", err)
	}
	modele, err := d.CreerModele(depot.Modele{Type: "DENSE", Annee: 2023, Code: "DENSE-2023"})
	if err != nil {
		t.Fatalf("seed modèle : %v", err)
	}
	revision, err = d.CreerRevision(depot.Revision{ModeleID: modele.ID, DateEffet: "2023-01-01"})
	if err != nil {
		t.Fatalf("seed révision : %v", err)
	}
	return dc, cluster, revision
}

// requeteAnalyserServeurs construit la requête multipart/form-data attendue
// par POST /import/serveurs/analyser. Le jeton CSRF est porté par l'en-tête,
// comme le pose hx-headers sur <body> pour toute requête htmx (voir le
// commentaire de tête de templates/import/serveurs.html).
func requeteAnalyserServeurs(t *testing.T, urlBase, jetonCSRF, contenuCSV string) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("fichier", "serveurs.csv")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write([]byte(contenuCSV)); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, urlBase+"/import/serveurs/analyser", &buf)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("X-CSRF-Token", jetonCSRF)
	return req
}

// extraireJeton relit le champ caché "jeton" du fragment de résultat, posé
// quand l'analyse ne remonte aucune erreur.
func extraireJeton(t *testing.T, frag string) string {
	t.Helper()
	marqueur := `name="jeton" value="`
	i := strings.Index(frag, marqueur)
	if i < 0 {
		t.Fatalf("jeton absent du fragment (analyse en erreur ?) : %s", frag)
	}
	i += len(marqueur)
	fin := strings.Index(frag[i:], `"`)
	if fin < 0 {
		t.Fatal("jeton mal formé dans le fragment")
	}
	return frag[i : i+fin]
}

// TestImportServeursValideAvecResolutionComplete couvre le cas nominal : une
// ligne complète (zone, affectation, rattachement de révision) est
// analysée sans erreur, puis confirmée — le serveur, son affectation et son
// rattachement doivent exister en base exactement comme décrit par la ligne.
func TestImportServeursValideAvecResolutionComplete(t *testing.T) {
	serveur, d := serveurDeTest(t)
	client := clientConnecte(t, serveur)
	dc, cluster, revision := seedImportServeursFixtures(t, d)

	repPage, err := client.Get(serveur.URL + "/import/serveurs")
	if err != nil {
		t.Fatal(err)
	}
	jeton := csrfDepuisPage(t, corps(t, repPage))

	csv := "physical_name;statut;date_entree;zone_code;projet_code;environnement_code;cluster_nom;modele_code\n" +
		"PHY001;EN_SERVICE;2023-03-01;DC1;LOGS;PROD;ElasticHot1;DENSE-2023\n"

	repAnalyse, err := client.Do(requeteAnalyserServeurs(t, serveur.URL, jeton, csv))
	if err != nil {
		t.Fatal(err)
	}
	if repAnalyse.StatusCode != http.StatusOK {
		t.Fatalf("analyse : attendu 200, obtenu %d", repAnalyse.StatusCode)
	}
	fragAnalyse := corps(t, repAnalyse)
	if strings.Contains(fragAnalyse, "message-erreur") {
		t.Fatalf("l'analyse d'une ligne valide ne devrait remonter aucune erreur : %s", fragAnalyse)
	}
	jetonImport := extraireJeton(t, fragAnalyse)

	// rien n'a encore été écrit après la seule analyse
	if serveurs, _ := d.ListerServeurs(depot.FiltreServeur{}); len(serveurs) != 0 {
		t.Fatalf("l'analyse seule ne doit rien écrire, obtenu %d serveur(s)", len(serveurs))
	}

	repConfirme, err := client.Do(requeteFormulaire(t, http.MethodPost, serveur.URL+"/import/serveurs/confirmer", jeton,
		url.Values{"jeton": {jetonImport}}))
	if err != nil {
		t.Fatal(err)
	}
	if repConfirme.StatusCode != http.StatusOK {
		t.Fatalf("confirmation : attendu 200, obtenu %d", repConfirme.StatusCode)
	}
	fragConfirme := corps(t, repConfirme)
	if !strings.Contains(fragConfirme, "1 serveur(s) importé(s) avec succès") {
		t.Fatalf("la confirmation devrait annoncer 1 serveur importé : %s", fragConfirme)
	}

	serveurs, err := d.ListerServeurs(depot.FiltreServeur{})
	if err != nil {
		t.Fatal(err)
	}
	if len(serveurs) != 1 {
		t.Fatalf("attendu 1 serveur en base, obtenu %d", len(serveurs))
	}
	srv := serveurs[0]
	if srv.PhysicalName == nil || *srv.PhysicalName != "PHY001" {
		t.Fatalf("physical_name attendu PHY001, obtenu %+v", srv.PhysicalName)
	}
	if srv.Statut != depot.StatutEnService {
		t.Fatalf("statut attendu EN_SERVICE, obtenu %s", srv.Statut)
	}
	if srv.ZoneID == nil || *srv.ZoneID != dc.ID {
		t.Fatalf("zone attendue %d, obtenu %+v", dc.ID, srv.ZoneID)
	}

	affectations, err := d.ListerAffectationsServeur(srv.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(affectations) != 1 || affectations[0].ClusterID != cluster.ID {
		t.Fatalf("affectation attendue vers le cluster %d, obtenu %+v", cluster.ID, affectations)
	}
	if affectations[0].DateDebut != "2023-03-01" {
		t.Fatalf("date d'affectation attendue 2023-03-01 (défaut date_entree), obtenu %s", affectations[0].DateDebut)
	}

	rattachements, err := d.ListerRattachements(srv.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rattachements) != 1 || rattachements[0].RevisionID != revision.ID {
		t.Fatalf("rattachement attendu vers la révision %d, obtenu %+v", revision.ID, rattachements)
	}
}

// TestImportServeursStatutHypotheseRejete couvre le refus explicite du
// statut HYPOTHESE, hors périmètre de l'import faute d'IHM de scénario.
func TestImportServeursStatutHypotheseRejete(t *testing.T) {
	serveur, d := serveurDeTest(t)
	client := clientConnecte(t, serveur)

	repPage, err := client.Get(serveur.URL + "/import/serveurs")
	if err != nil {
		t.Fatal(err)
	}
	jeton := csrfDepuisPage(t, corps(t, repPage))

	csv := "physical_name;statut;date_entree\nPHY002;HYPOTHESE;2023-01-01\n"

	rep, err := client.Do(requeteAnalyserServeurs(t, serveur.URL, jeton, csv))
	if err != nil {
		t.Fatal(err)
	}
	if rep.StatusCode != http.StatusOK {
		t.Fatalf("attendu 200, obtenu %d", rep.StatusCode)
	}
	// html/template échappe l'apostrophe de "l'import" en &#39; dans un nœud
	// texte : on vérifie autour plutôt que la ponctuation exacte.
	frag := corps(t, rep)
	if !strings.Contains(frag, "HYPOTHESE exige un scénario, non supporté") || !strings.Contains(frag, "import") {
		t.Fatalf("le message dédié à HYPOTHESE devrait apparaître : %s", frag)
	}
	if !strings.Contains(frag, "Ligne 2") {
		t.Fatalf("l'erreur devrait être rattachée à la ligne 2 : %s", frag)
	}

	if serveurs, _ := d.ListerServeurs(depot.FiltreServeur{}); len(serveurs) != 0 {
		t.Fatalf("aucun serveur ne devrait être créé, obtenu %d", len(serveurs))
	}
}

// TestImportServeursClusterInexistantAucunEcriture est le test clé du
// critère d'acceptation v1.4 : un fichier mêlant des lignes valides et une
// ligne dont le cluster_nom ne correspond à rien ne doit écrire STRICTEMENT
// RIEN, y compris pour les lignes par ailleurs valides.
func TestImportServeursClusterInexistantAucunEcriture(t *testing.T) {
	serveur, d := serveurDeTest(t)
	client := clientConnecte(t, serveur)
	_, _, _ = seedImportServeursFixtures(t, d)

	repPage, err := client.Get(serveur.URL + "/import/serveurs")
	if err != nil {
		t.Fatal(err)
	}
	jeton := csrfDepuisPage(t, corps(t, repPage))

	if avant, _ := d.ListerServeurs(depot.FiltreServeur{}); len(avant) != 0 {
		t.Fatalf("précondition : base vide attendue, obtenu %d serveur(s)", len(avant))
	}

	csv := "physical_name;statut;date_entree;projet_code;environnement_code;cluster_nom\n" +
		"PHY010;EN_SERVICE;2023-01-01;LOGS;PROD;ElasticHot1\n" +
		"PHY011;EN_SERVICE;2023-01-02;LOGS;PROD;ElasticHot1\n" +
		"PHY012;EN_SERVICE;2023-01-03;LOGS;PROD;ClusterInexistant\n"

	rep, err := client.Do(requeteAnalyserServeurs(t, serveur.URL, jeton, csv))
	if err != nil {
		t.Fatal(err)
	}
	if rep.StatusCode != http.StatusOK {
		t.Fatalf("attendu 200, obtenu %d", rep.StatusCode)
	}
	frag := corps(t, rep)
	if !strings.Contains(frag, "Ligne 4") {
		t.Fatalf("l'erreur devrait être rattachée à la ligne 4 (3e ligne de données) : %s", frag)
	}
	if !strings.Contains(frag, "cluster introuvable") || !strings.Contains(frag, "ClusterInexistant") {
		t.Fatalf("le message devrait nommer le cluster introuvable : %s", frag)
	}
	if strings.Contains(frag, `name="jeton"`) {
		t.Fatal("aucun jeton de confirmation ne devrait être proposé quand le rapport porte une erreur")
	}

	apres, err := d.ListerServeurs(depot.FiltreServeur{})
	if err != nil {
		t.Fatal(err)
	}
	if len(apres) != 0 {
		t.Fatalf("aucun serveur ne devrait être créé, même pour les 2 lignes par ailleurs valides, obtenu %d", len(apres))
	}
}

// TestImportServeursAffectationPartielleEstUneErreur couvre la règle « les
// trois colonnes projet_code/environnement_code/cluster_nom ensemble, ou
// aucune » : une seule renseignée est une erreur de ligne, pas une absence
// silencieuse d'affectation.
func TestImportServeursAffectationPartielleEstUneErreur(t *testing.T) {
	serveur, d := serveurDeTest(t)
	client := clientConnecte(t, serveur)

	repPage, err := client.Get(serveur.URL + "/import/serveurs")
	if err != nil {
		t.Fatal(err)
	}
	jeton := csrfDepuisPage(t, corps(t, repPage))

	csv := "physical_name;statut;date_entree;projet_code\nPHY020;EN_SERVICE;2023-01-01;LOGS\n"

	rep, err := client.Do(requeteAnalyserServeurs(t, serveur.URL, jeton, csv))
	if err != nil {
		t.Fatal(err)
	}
	if rep.StatusCode != http.StatusOK {
		t.Fatalf("attendu 200, obtenu %d", rep.StatusCode)
	}
	frag := corps(t, rep)
	if !strings.Contains(frag, "doivent être renseignés tous les trois, ou aucun") {
		t.Fatalf("le message dédié à l'affectation partielle devrait apparaître : %s", frag)
	}

	if serveurs, _ := d.ListerServeurs(depot.FiltreServeur{}); len(serveurs) != 0 {
		t.Fatalf("aucun serveur ne devrait être créé, obtenu %d", len(serveurs))
	}
}
