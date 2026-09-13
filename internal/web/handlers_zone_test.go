package web

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"parallax/internal/depot"
)

func TestParcoursZoneCompletParHTTP(t *testing.T) {
	serveur, d := serveurDeTest(t)
	client := clientConnecte(t, serveur)
	jeton := jetonCSRF(t, client, serveur.URL, "/zones")

	// création, site renseigné
	req, _ := http.NewRequest(http.MethodPost, serveur.URL+"/zones",
		strings.NewReader(url.Values{"code": {"DC1"}, "libelle": {"Zone 1"}, "site": {"Paris"}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-CSRF-Token", jeton)
	repCreation, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if repCreation.StatusCode != http.StatusOK {
		t.Fatalf("création : attendu 200, obtenu %d", repCreation.StatusCode)
	}
	if fragment := corps(t, repCreation); !strings.Contains(fragment, "DC1") {
		t.Fatalf("la ligne créée devrait apparaître dans le fragment : %s", fragment)
	}

	zones, err := d.ListerZones()
	if err != nil {
		t.Fatal(err)
	}
	if len(zones) != 1 || zones[0].Code != "DC1" ||
		zones[0].Site == nil || *zones[0].Site != "Paris" {
		t.Fatalf("la zone aurait dû être créé en base avec son site : %+v", zones)
	}
	id := zones[0].ID
	idStr := strconv.FormatInt(id, 10)

	// lecture de la liste
	repListe, err := client.Get(serveur.URL + "/zones")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(corps(t, repListe), "Paris") {
		t.Fatal("le site devrait apparaître dans la page")
	}

	// modification, site vidé : doit repasser à nil, pas chaîne vide
	req, _ = http.NewRequest(http.MethodPut, serveur.URL+"/zones/"+idStr,
		strings.NewReader(url.Values{"code": {"DC1"}, "libelle": {"Zone 1 (modifié)"}, "site": {""}}.Encode()))
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
	relu, err := d.LireZone(id)
	if err != nil {
		t.Fatal(err)
	}
	if relu.Site != nil {
		t.Fatalf("le site vidé aurait dû repasser à nil, obtenu %+v", relu.Site)
	}

	// conflit de code : 200 (pas 4xx), message inline
	if _, err := d.CreerZone(depot.Zone{Code: "DC2", Libelle: "Zone 2"}); err != nil {
		t.Fatal(err)
	}
	req, _ = http.NewRequest(http.MethodPut, serveur.URL+"/zones/"+idStr,
		strings.NewReader(url.Values{"code": {"DC2"}, "libelle": {"doublon"}}.Encode()))
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
