package web

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"parallax/internal/depot"
)

func TestParcoursUsageCompletParHTTP(t *testing.T) {
	serveur, d := serveurDeTest(t)
	client := clientConnecte(t, serveur)
	jeton := jetonCSRF(t, client, serveur.URL, "/usages")

	// création
	req, _ := http.NewRequest(http.MethodPost, serveur.URL+"/usages",
		strings.NewReader(url.Values{"code": {"LOGS"}, "libelle": {"Log management"}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-CSRF-Token", jeton)
	repCreation, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if repCreation.StatusCode != http.StatusOK {
		t.Fatalf("création : attendu 200, obtenu %d", repCreation.StatusCode)
	}
	if fragment := corps(t, repCreation); !strings.Contains(fragment, "LOGS") {
		t.Fatalf("la ligne créée devrait apparaître dans le fragment : %s", fragment)
	}

	usages, err := d.ListerUsages()
	if err != nil {
		t.Fatal(err)
	}
	if len(usages) != 1 || usages[0].Code != "LOGS" {
		t.Fatalf("l'usage aurait dû être créé en base : %+v", usages)
	}
	id := usages[0].ID
	idStr := strconv.FormatInt(id, 10)

	// lecture de la liste
	repListe, err := client.Get(serveur.URL + "/usages")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(corps(t, repListe), "Log management") {
		t.Fatal("le libellé devrait apparaître dans la page")
	}

	// modification
	req, _ = http.NewRequest(http.MethodPut, serveur.URL+"/usages/"+idStr,
		strings.NewReader(url.Values{"code": {"LOGS"}, "libelle": {"Log management (modifié)"}}.Encode()))
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
	if _, err := d.CreerUsage(depot.UsageFonctionnel{Code: "APM", Libelle: "APM"}); err != nil {
		t.Fatal(err)
	}
	req, _ = http.NewRequest(http.MethodPut, serveur.URL+"/usages/"+idStr,
		strings.NewReader(url.Values{"code": {"APM"}, "libelle": {"doublon"}}.Encode()))
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
}
