package web

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"parallax/internal/depot"
)

func jetonCSRF(t *testing.T, client *http.Client, serveur string, chemin string) string {
	t.Helper()
	rep, err := client.Get(serveur + chemin)
	if err != nil {
		t.Fatal(err)
	}
	page := corps(t, rep)
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

func TestParcoursTechnoCompletParHTTP(t *testing.T) {
	serveur, d := serveurDeTest(t)
	client := clientConnecte(t, serveur)
	jeton := jetonCSRF(t, client, serveur.URL, "/technos")

	// création
	req, _ := http.NewRequest(http.MethodPost, serveur.URL+"/technos",
		strings.NewReader(url.Values{"code": {"ELASTIC"}, "libelle": {"Elasticsearch"}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-CSRF-Token", jeton)
	repCreation, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if repCreation.StatusCode != http.StatusOK {
		t.Fatalf("création : attendu 200, obtenu %d", repCreation.StatusCode)
	}
	if fragment := corps(t, repCreation); !strings.Contains(fragment, "ELASTIC") {
		t.Fatalf("la ligne créée devrait apparaître dans le fragment : %s", fragment)
	}

	technos, err := d.ListerTechnos(false)
	if err != nil {
		t.Fatal(err)
	}
	if len(technos) != 1 || technos[0].Code != "ELASTIC" {
		t.Fatalf("la techno aurait dû être créée en base : %+v", technos)
	}
	id := technos[0].ID
	idStr := strconv.FormatInt(id, 10)

	// lecture de la liste
	repListe, err := client.Get(serveur.URL + "/technos")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(corps(t, repListe), "Elasticsearch") {
		t.Fatal("le libellé devrait apparaître dans la page")
	}

	// modification
	req, _ = http.NewRequest(http.MethodPut, serveur.URL+"/technos/"+idStr,
		strings.NewReader(url.Values{"code": {"ELASTIC"}, "libelle": {"Elasticsearch (modifié)"}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-CSRF-Token", jeton)
	repModif, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if repModif.StatusCode != http.StatusOK {
		t.Fatalf("modification : attendu 200, obtenu %d", repModif.StatusCode)
	}
	if !strings.Contains(corps(t, repModif), "modifié") {
		t.Fatal("le libellé modifié devrait apparaître dans le fragment")
	}

	// conflit de code : 200 (pas 4xx), message inline
	if _, err := d.CreerTechno(depot.Techno{Code: "KAFKA", Libelle: "Kafka"}); err != nil {
		t.Fatal(err)
	}
	req, _ = http.NewRequest(http.MethodPut, serveur.URL+"/technos/"+idStr,
		strings.NewReader(url.Values{"code": {"KAFKA"}, "libelle": {"doublon"}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-CSRF-Token", jeton)
	repConflit, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if repConflit.StatusCode != http.StatusOK {
		t.Fatalf("conflit métier : attendu 200, obtenu %d", repConflit.StatusCode)
	}
	if !strings.Contains(corps(t, repConflit), "existe déjà") {
		t.Fatal("le message de conflit devrait apparaître dans le fragment")
	}

	// cycle archivage / réactivation
	req, _ = http.NewRequest(http.MethodPost, serveur.URL+"/technos/"+idStr+"/archiver", nil)
	req.Header.Set("X-CSRF-Token", jeton)
	repArchive, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if repArchive.StatusCode != http.StatusOK {
		t.Fatalf("archivage : attendu 200, obtenu %d", repArchive.StatusCode)
	}
	if !strings.Contains(corps(t, repArchive), "archivé") {
		t.Fatal("la ligne archivée devrait afficher le statut archivé")
	}
	actuel, err := d.LireTechno(id)
	if err != nil {
		t.Fatal(err)
	}
	if actuel.Actif {
		t.Fatal("la techno devrait être inactive après archivage")
	}

	req, _ = http.NewRequest(http.MethodPost, serveur.URL+"/technos/"+idStr+"/reactiver", nil)
	req.Header.Set("X-CSRF-Token", jeton)
	repReactive, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if repReactive.StatusCode != http.StatusOK {
		t.Fatalf("réactivation : attendu 200, obtenu %d", repReactive.StatusCode)
	}
	actuel, err = d.LireTechno(id)
	if err != nil {
		t.Fatal(err)
	}
	if !actuel.Actif {
		t.Fatal("la techno devrait être active après réactivation")
	}
}
