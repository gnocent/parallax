package web

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"parallax/internal/depot"
)

// Tests des écrans d'adressage (backlog v3.1) : page des anomalies avec ses
// compteurs et son export, aperçu puis attribution en lot sur une
// hypothèse, choix du VLAN serveur par serveur.

// serveurAvec crée un serveur réel EN_SERVICE avec adresse, VLAN et zone,
// affecté au cluster (0 = sans affectation).
func serveurAvec(t *testing.T, d *depot.Depot, nom, ip string, vlanID, zoneID, clusterID int64) int64 {
	t.Helper()
	s := depot.Serveur{PhysicalName: &nom, Statut: depot.StatutEnService}
	if ip != "" {
		s.IP = &ip
	}
	if vlanID != 0 {
		s.VlanID = &vlanID
	}
	if zoneID != 0 {
		s.ZoneID = &zoneID
	}
	cree, err := d.CreerServeur(s)
	if err != nil {
		t.Fatal(err)
	}
	if clusterID != 0 {
		if err := d.Affecter(cree.ID, clusterID, "2026-01-01", nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	return cree.ID
}

func TestAdressageAnomaliesPageEtExport(t *testing.T) {
	srv, d := serveurAdressageDeTest(t)
	r := semerReseauDeTest(t, d)
	client := clientConnecte(t, srv)

	prod, err := d.CreerVlan(depot.Vlan{Code: "VL-PROD", EnvironnementID: &r.Prod})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.CreerPlage(depot.PlageIP{VlanID: prod.ID, IPDebut: "10.0.0.1", IPFin: "10.0.0.50"}); err != nil {
		t.Fatal(err)
	}
	ok := serveurAvec(t, d, "ok-01", "10.0.0.5", prod.ID, r.DC1, r.ClusterHot)
	dup1 := serveurAvec(t, d, "dup-01", "10.0.0.7", prod.ID, r.DC1, r.ClusterHot)
	dup2 := serveurAvec(t, d, "dup-02", "10.0.0.7", prod.ID, r.DC1, r.ClusterHot)
	hors := serveurAvec(t, d, "hors-01", "10.0.0.99", prod.ID, r.DC1, r.ClusterHot)
	sansVlan := serveurAvec(t, d, "sans-vlan", "10.0.0.9", 0, r.DC1, r.ClusterHot)

	rep, err := client.Get(srv.URL + "/adressage/anomalies")
	if err != nil {
		t.Fatal(err)
	}
	page := corps(t, rep)
	if rep.StatusCode != http.StatusOK {
		t.Fatalf("anomalies : %d %s", rep.StatusCode, page)
	}
	for _, attendu := range []string{
		`<strong style="font-size:1.4em;">4</strong> anomalie(s)`,
		`href="/serveurs/` + id64(dup1) + `"`, `href="/serveurs/` + id64(dup2) + `"`,
		`href="/serveurs/` + id64(hors) + `"`, `href="/serveurs/` + id64(sansVlan) + `"`,
		"adresse aussi portée par dup-02", "hors plage", "sans VLAN",
		`href="/adressage/anomalies?export=csv"`,
	} {
		if !strings.Contains(page, attendu) {
			t.Fatalf("attendu « %s » dans la page : %s", attendu, page)
		}
	}
	if strings.Contains(page, `href="/serveurs/`+id64(ok)+`"`) {
		t.Fatal("un serveur sans anomalie ne doit pas être listé")
	}

	repCSV, _ := client.Get(srv.URL + "/adressage/anomalies?export=csv")
	csv := corps(t, repCSV)
	if !strings.HasPrefix(csv, "Serveur;Adresse;VLAN;Type;Détail\n") || !strings.Contains(csv, "hors-01;10.0.0.99;VL-PROD;hors plage;") {
		t.Fatalf("export csv inattendu : %s", csv)
	}

	// regroupement par serveur, pour le repère sur la fiche et la ligne
	parServeur, err := (&serveur{depot: d}).anomaliesParServeur()
	if err != nil {
		t.Fatal(err)
	}
	if len(parServeur) != 4 || len(parServeur[dup1]) != 1 || parServeur[dup1][0].Type != depot.AnomalieAdresseMultiple || len(parServeur[ok]) != 0 {
		t.Fatalf("regroupement inattendu : %+v", parServeur)
	}
}

func TestAdressageScenarioApercuPuisAttribution(t *testing.T) {
	serveur, d := serveurAdressageDeTest(t)
	r := semerReseauDeTest(t, d)
	client := clientConnecte(t, serveur)
	jeton := jetonCSRFDepuisPage(t, client, serveur, "/vlans")

	dc1, err := d.CreerVlan(depot.Vlan{Code: "VL-DC1", EnvironnementID: &r.Prod, ZoneID: &r.DC1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.CreerPlage(depot.PlageIP{VlanID: dc1.ID, IPDebut: "10.1.0.10", IPFin: "10.1.0.20"}); err != nil {
		t.Fatal(err)
	}
	// deux VLAN candidats en DC2 : le serveur devra être choisi
	dc2a, _ := d.CreerVlan(depot.Vlan{Code: "VL-DC2-A", EnvironnementID: &r.Prod, ZoneID: &r.DC2})
	dc2b, _ := d.CreerVlan(depot.Vlan{Code: "VL-DC2-B", EnvironnementID: &r.Prod, ZoneID: &r.DC2})
	for _, v := range []depot.Vlan{dc2a, dc2b} {
		if _, err := d.CreerPlage(depot.PlageIP{VlanID: v.ID, IPDebut: "10.2.0.10", IPFin: "10.2.0.20"}); err != nil {
			t.Fatal(err)
		}
	}
	// une adresse déjà prise dans le réel : le pool est global
	serveurAvec(t, d, "reel-01", "10.1.0.10", dc1.ID, r.DC1, r.ClusterHot)

	sc, err := d.CreerScenario(depot.Scenario{Nom: "Extension 2027", Description: "test"})
	if err != nil {
		t.Fatal(err)
	}
	hypothese := func(nom string, zoneID int64) int64 {
		t.Helper()
		s, err := d.CreerServeur(depot.Serveur{PhysicalName: &nom, Statut: depot.StatutHypothese, ScenarioID: &sc.ID, ZoneID: &zoneID})
		if err != nil {
			t.Fatal(err)
		}
		if err := d.Affecter(s.ID, r.ClusterHot, "2027-01-01", &sc.ID, nil); err != nil {
			t.Fatal(err)
		}
		return s.ID
	}
	h1 := hypothese("hyp-dc1-01", r.DC1)
	h2 := hypothese("hyp-dc2-01", r.DC2)
	chemin := "/adressage/scenarios/" + id64(sc.ID)

	// aperçu : rien n'est écrit, la page annonce ce qui se passerait
	rep, err := client.Get(serveur.URL + chemin)
	if err != nil {
		t.Fatal(err)
	}
	page := corps(t, rep)
	if rep.StatusCode != http.StatusOK || !strings.Contains(page, "Adresser les serveurs de cette hypothèse") ||
		!strings.Contains(page, "<strong>1</strong> attribuée") || !strings.Contains(page, "<strong>1</strong> à choisir") ||
		!strings.Contains(page, `href="`+chemin+`?export=csv"`) {
		t.Fatalf("aperçu inattendu : %d %s", rep.StatusCode, page)
	}
	if s, _ := d.LireServeur(h1); s.IP != nil {
		t.Fatal("l'aperçu ne doit rien écrire")
	}

	// attribution : formulaire classique avec _csrf
	req, _ := http.NewRequest(http.MethodPost, serveur.URL+chemin, strings.NewReader(url.Values{"_csrf": {jeton}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rep, err = client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	page = corps(t, rep)
	if rep.StatusCode != http.StatusOK || !strings.Contains(page, "Attribution effectuée") || !strings.Contains(page, "10.1.0.11") {
		t.Fatalf("attribution : %d %s", rep.StatusCode, page)
	}
	s1, _ := d.LireServeur(h1)
	if s1.IP == nil || *s1.IP != "10.1.0.11" || s1.VlanID == nil || *s1.VlanID != dc1.ID {
		t.Fatalf("hyp-dc1-01 : 10.1.0.11 dans VL-DC1 attendue (10.1.0.10 prise par le réel), obtenu %+v", s1)
	}
	// la ligne « à choisir » porte le formulaire de choix
	if !strings.Contains(page, `action="/adressage/serveurs/`+id64(h2)+`"`) || !strings.Contains(page, `<option value="`+id64(dc2b.ID)+`">VL-DC2-B</option>`) {
		t.Fatalf("formulaire de choix du VLAN attendu pour hyp-dc2-01 : %s", page)
	}

	// export après coup : l'état courant, serveur déjà adressé compris
	repCSV, _ := client.Get(serveur.URL + chemin + "?export=csv")
	csv := corps(t, repCSV)
	if !strings.HasPrefix(csv, "Serveur;Cluster;Issue;Adresse;VLAN;Détail\n") || !strings.Contains(csv, "hyp-dc1-01;ElasticHot;déjà adressé;10.1.0.11;VL-DC1;") ||
		!strings.Contains(csv, "hyp-dc2-01;ElasticHot;à choisir;;;") {
		t.Fatalf("export csv inattendu : %s", csv)
	}

	// choix du VLAN pour le serveur : redirection vers la page indiquée (suite), sinon la fiche
	req, _ = http.NewRequest(http.MethodPost, serveur.URL+"/adressage/serveurs/"+id64(h2),
		strings.NewReader(url.Values{"_csrf": {jeton}, "vlan_id": {id64(dc2b.ID)}, "suite": {chemin}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rep, _ = client.Do(req)
	rep.Body.Close()
	if rep.StatusCode != http.StatusSeeOther || rep.Header.Get("Location") != chemin {
		t.Fatalf("attendu 303 vers %s, obtenu %d %s", chemin, rep.StatusCode, rep.Header.Get("Location"))
	}
	s2, _ := d.LireServeur(h2)
	if s2.IP == nil || *s2.IP != "10.2.0.10" || s2.VlanID == nil || *s2.VlanID != dc2b.ID {
		t.Fatalf("hyp-dc2-01 : 10.2.0.10 dans VL-DC2-B attendue, obtenu %+v", s2)
	}
	// second choix : déjà adressé -> 303 vers la fiche avec le message
	req, _ = http.NewRequest(http.MethodPost, serveur.URL+"/adressage/serveurs/"+id64(h2),
		strings.NewReader(url.Values{"_csrf": {jeton}, "vlan_id": {id64(dc2a.ID)}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rep, _ = client.Do(req)
	rep.Body.Close()
	loc := rep.Header.Get("Location")
	if rep.StatusCode != http.StatusSeeOther || !strings.HasPrefix(loc, "/serveurs/"+id64(h2)+"?erreur_adressage=") || !strings.Contains(loc, "10.2.0.10") {
		t.Fatalf("attendu 303 vers la fiche avec erreur_adressage, obtenu %d %s", rep.StatusCode, loc)
	}

	// scénario inconnu : 404 ; abandonné : message à la place du tableau
	rep, _ = client.Get(serveur.URL + "/adressage/scenarios/999")
	rep.Body.Close()
	if rep.StatusCode != http.StatusNotFound {
		t.Fatalf("scénario inconnu : 404 attendu, obtenu %d", rep.StatusCode)
	}
	if err := d.AbandonnerScenario(sc.ID); err != nil {
		t.Fatal(err)
	}
	rep, _ = client.Get(serveur.URL + chemin)
	if page = corps(t, rep); rep.StatusCode != http.StatusOK || !strings.Contains(page, "scénario abandonné") {
		t.Fatalf("scénario abandonné : message attendu, obtenu %d %s", rep.StatusCode, page)
	}
}
