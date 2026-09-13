package web

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"parallax/internal/depot"
)

// csrfDepuisPage extrait le jeton CSRF embarqué par hx-headers sur <body>,
// comme le fait TestParcoursProjetCompletParHTTP en ligne.
func csrfDepuisPage(t *testing.T, page string) string {
	t.Helper()
	i := strings.Index(page, `X-CSRF-Token":"`)
	if i < 0 {
		t.Fatal("jeton CSRF absent de la page")
	}
	i += len(`X-CSRF-Token":"`)
	jeton := page[i : strings.Index(page[i:], `"`)+i]
	if jeton == "" {
		t.Fatal("jeton CSRF vide")
	}
	return jeton
}

// seedRefsCluster sème un projet, un environnement et une techno minimaux,
// suffisants pour créer un cluster.
func seedRefsCluster(t *testing.T, d *depot.Depot) (projetID, envID, technoID int64) {
	t.Helper()
	p, err := d.CreerProjet(depot.Projet{Code: "LOGS", Libelle: "Log Management"})
	if err != nil {
		t.Fatalf("seed projet : %v", err)
	}
	e, err := d.CreerEnvironnement(depot.Environnement{Code: "PROD", Libelle: "Production", Ordre: 1})
	if err != nil {
		t.Fatalf("seed environnement : %v", err)
	}
	tc, err := d.CreerTechno(depot.Techno{Code: "ELASTIC", Libelle: "Elasticsearch"})
	if err != nil {
		t.Fatalf("seed techno : %v", err)
	}
	return p.ID, e.ID, tc.ID
}

func requeteFormulaire(t *testing.T, methode, url_, jeton string, valeurs url.Values) *http.Request {
	t.Helper()
	req, err := http.NewRequest(methode, url_, strings.NewReader(valeurs.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-CSRF-Token", jeton)
	return req
}

func TestParcoursClusterCompletParHTTP(t *testing.T) {
	serveur, d := serveurDeTest(t)
	client := clientConnecte(t, serveur)
	projetID, envID, technoID := seedRefsCluster(t, d)

	repPage, err := client.Get(serveur.URL + "/clusters")
	if err != nil {
		t.Fatal(err)
	}
	jeton := csrfDepuisPage(t, corps(t, repPage))

	champs := url.Values{
		"nom":              {"ElasticHot1"},
		"projet_id":        {strconv.FormatInt(projetID, 10)},
		"environnement_id": {strconv.FormatInt(envID, 10)},
		"techno_id":        {strconv.FormatInt(technoID, 10)},
	}

	// création
	repCreation, err := client.Do(requeteFormulaire(t, http.MethodPost, serveur.URL+"/clusters", jeton, champs))
	if err != nil {
		t.Fatal(err)
	}
	if repCreation.StatusCode != http.StatusOK {
		t.Fatalf("création : attendu 200, obtenu %d", repCreation.StatusCode)
	}
	fragment := corps(t, repCreation)
	if !strings.Contains(fragment, "ElasticHot1") {
		t.Fatalf("la ligne créée devrait apparaître dans le fragment : %s", fragment)
	}
	if !strings.Contains(fragment, "Log Management") || !strings.Contains(fragment, "Elasticsearch") {
		t.Fatalf("le tableau doit afficher les LIBELLÉS des dimensions, pas seulement leurs id : %s", fragment)
	}

	clusters, err := d.ListerClusters(depot.FiltreCluster{})
	if err != nil {
		t.Fatal(err)
	}
	if len(clusters) != 1 || clusters[0].Nom != "ElasticHot1" {
		t.Fatalf("le cluster aurait dû être créé en base : %+v", clusters)
	}
	clusterID := clusters[0].ID

	// conflit d'unicité (projet, environnement, nom) : 200, message inline
	repConflit, err := client.Do(requeteFormulaire(t, http.MethodPost, serveur.URL+"/clusters", jeton, champs))
	if err != nil {
		t.Fatal(err)
	}
	if repConflit.StatusCode != http.StatusOK {
		t.Fatalf("conflit métier : attendu 200, obtenu %d", repConflit.StatusCode)
	}
	if !strings.Contains(corps(t, repConflit), "existe déjà") {
		t.Fatal("le message de conflit devrait apparaître dans le fragment")
	}

	// filtre par projet : retrouve le cluster
	repFiltre, err := client.Get(serveur.URL + "/clusters/tableau?filtre_projet_id=" + strconv.FormatInt(projetID, 10))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(corps(t, repFiltre), "ElasticHot1") {
		t.Fatal("le filtre par projet devrait retrouver le cluster")
	}

	// filtre par un autre projet : ne retrouve rien
	repFiltreVide, err := client.Get(serveur.URL + "/clusters/tableau?filtre_projet_id=999")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(corps(t, repFiltreVide), "ElasticHot1") {
		t.Fatal("un filtre sur un autre projet ne devrait pas retrouver le cluster")
	}

	// modification
	champsModifies := url.Values{
		"nom":              {"ElasticHot1-bis"},
		"projet_id":        {strconv.FormatInt(projetID, 10)},
		"environnement_id": {strconv.FormatInt(envID, 10)},
		"techno_id":        {strconv.FormatInt(technoID, 10)},
	}
	repMod, err := client.Do(requeteFormulaire(t, http.MethodPut,
		serveur.URL+"/clusters/"+strconv.FormatInt(clusterID, 10), jeton, champsModifies))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(corps(t, repMod), "ElasticHot1-bis") {
		t.Fatal("la modification devrait être reflétée dans la ligne renvoyée")
	}

	// archivage puis réactivation
	repArch, err := client.Do(requeteFormulaire(t, http.MethodPost,
		serveur.URL+"/clusters/"+strconv.FormatInt(clusterID, 10)+"/archiver", jeton, nil))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(corps(t, repArch), "archivé") {
		t.Fatal("le cluster archivé devrait porter le badge archivé")
	}
	repReact, err := client.Do(requeteFormulaire(t, http.MethodPost,
		serveur.URL+"/clusters/"+strconv.FormatInt(clusterID, 10)+"/reactiver", jeton, nil))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(corps(t, repReact), "actif") {
		t.Fatal("le cluster réactivé devrait porter le badge actif")
	}
}
