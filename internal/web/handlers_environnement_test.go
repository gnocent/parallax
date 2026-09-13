package web

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"parallax/internal/depot"
)

func TestParcoursEnvironnementCompletParHTTP(t *testing.T) {
	serveur, d := serveurDeTest(t)
	client := clientConnecte(t, serveur)
	jeton := jetonCSRF(t, client, serveur.URL, "/environnements")

	// création
	req, _ := http.NewRequest(http.MethodPost, serveur.URL+"/environnements",
		strings.NewReader(url.Values{"code": {"PROD"}, "libelle": {"Production"}, "ordre": {"1"}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-CSRF-Token", jeton)
	repCreation, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if repCreation.StatusCode != http.StatusOK {
		t.Fatalf("création : attendu 200, obtenu %d", repCreation.StatusCode)
	}
	if fragment := corps(t, repCreation); !strings.Contains(fragment, "PROD") {
		t.Fatalf("la ligne créée devrait apparaître dans le fragment : %s", fragment)
	}

	environnements, err := d.ListerEnvironnements()
	if err != nil {
		t.Fatal(err)
	}
	if len(environnements) != 1 || environnements[0].Code != "PROD" || environnements[0].Ordre != 1 {
		t.Fatalf("l'environnement aurait dû être créé en base : %+v", environnements)
	}
	id := environnements[0].ID
	idStr := strconv.FormatInt(id, 10)

	// lecture de la liste
	repListe, err := client.Get(serveur.URL + "/environnements")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(corps(t, repListe), "Production") {
		t.Fatal("le libellé devrait apparaître dans la page")
	}

	// modification
	req, _ = http.NewRequest(http.MethodPut, serveur.URL+"/environnements/"+idStr,
		strings.NewReader(url.Values{"code": {"PROD"}, "libelle": {"Production (modifié)"}, "ordre": {"2"}}.Encode()))
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
	relu, err := d.LireEnvironnement(id)
	if err != nil {
		t.Fatal(err)
	}
	if relu.Ordre != 2 {
		t.Fatalf("l'ordre aurait dû être mis à jour : %+v", relu)
	}

	// conflit de code : 200 (pas 4xx), message inline
	if _, err := d.CreerEnvironnement(depot.Environnement{Code: "QUAL", Libelle: "Qualification"}); err != nil {
		t.Fatal(err)
	}
	req, _ = http.NewRequest(http.MethodPut, serveur.URL+"/environnements/"+idStr,
		strings.NewReader(url.Values{"code": {"QUAL"}, "libelle": {"doublon"}, "ordre": {"1"}}.Encode()))
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
