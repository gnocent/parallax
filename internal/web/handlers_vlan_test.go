package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"parallax/internal/auth"
	"parallax/internal/db"
	"parallax/internal/depot"
)

// serveurAdressageDeTest est l'équivalent de serveurDeTest (web_test.go)
// pour le lot v3.1, construit sur le modèle de serveurImportModelesDeTest :
// les routes VLAN, import VLAN et adressage ne sont pas encore câblées dans
// server.go (l'intégration ajoute les appels), on les enregistre ici via les
// mêmes fonctions routesXxx(). Le jour où routes() les appelle, ces tests
// restent valides tels quels.
func serveurAdressageDeTest(t *testing.T) (*httptest.Server, *depot.Depot) {
	t.Helper()
	chemin := filepath.Join(t.TempDir(), "web-test-adressage.db")
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
		Login: "editeur", Hash: hash, Role: depot.RoleEditeur,
	}); err != nil {
		t.Fatal(err)
	}

	svc := auth.NouveauService(d)
	s := &serveur{mux: http.NewServeMux(), gabarits: chargerGabarits(), depot: d, auth: svc}
	s.mux.HandleFunc("GET /connexion", s.connexionFormulaire)
	s.mux.HandleFunc("POST /connexion", s.connexionSoumettre)
	s.mux.HandleFunc("POST /deconnexion", s.exigerAuth(s.verifierCSRF(s.deconnexionSoumettre)))
	s.routesVlans()
	s.routesImportVlans()
	s.routesAdressage()

	handler := recuperer(journaliser(s.chargerSession(s.mux)))
	serveurHTTP := httptest.NewServer(handler)
	t.Cleanup(serveurHTTP.Close)
	return serveurHTTP, d
}

// refsReseau regroupe les identifiants semés par semerReseauDeTest.
type refsReseau struct {
	Projet, Prod, Preprod, Techno, DC1, DC2 int64
	ClusterHot, ClusterKafka                int64
}

// semerReseauDeTest pose un jeu minimal cohérent avec exemples/*.csv :
// projet LOGS, environnements PROD et PREPROD, zones DC1 et DC2, clusters
// ElasticHot (PROD) et Kafka (PROD).
func semerReseauDeTest(t *testing.T, d *depot.Depot) refsReseau {
	t.Helper()
	var r refsReseau
	p, err := d.CreerProjet(depot.Projet{Code: "LOGS", Libelle: "Plateforme de logs"})
	if err != nil {
		t.Fatal(err)
	}
	r.Projet = p.ID
	prod, err := d.CreerEnvironnement(depot.Environnement{Code: "PROD", Libelle: "Production", Ordre: 1})
	if err != nil {
		t.Fatal(err)
	}
	r.Prod = prod.ID
	preprod, err := d.CreerEnvironnement(depot.Environnement{Code: "PREPROD", Libelle: "Pré-production", Ordre: 2})
	if err != nil {
		t.Fatal(err)
	}
	r.Preprod = preprod.ID
	techno, err := d.CreerTechno(depot.Techno{Code: "ELASTIC", Libelle: "Elasticsearch"})
	if err != nil {
		t.Fatal(err)
	}
	r.Techno = techno.ID
	dc1, err := d.CreerZone(depot.Zone{Code: "DC1", Libelle: "Zone 1"})
	if err != nil {
		t.Fatal(err)
	}
	r.DC1 = dc1.ID
	dc2, err := d.CreerZone(depot.Zone{Code: "DC2", Libelle: "Zone 2"})
	if err != nil {
		t.Fatal(err)
	}
	r.DC2 = dc2.ID
	hot, err := d.CreerCluster(depot.Cluster{Nom: "ElasticHot", ProjetID: p.ID, EnvironnementID: prod.ID, TechnoID: techno.ID})
	if err != nil {
		t.Fatal(err)
	}
	r.ClusterHot = hot.ID
	kafka, err := d.CreerCluster(depot.Cluster{Nom: "Kafka", ProjetID: p.ID, EnvironnementID: prod.ID, TechnoID: techno.ID})
	if err != nil {
		t.Fatal(err)
	}
	r.ClusterKafka = kafka.ID
	return r
}

// requeteHTMX envoie une requête de mutation comme le ferait htmx : corps
// url-encodé, jeton CSRF en en-tête, marqueur HX-Request.
func requeteHTMX(t *testing.T, client *http.Client, serveur *httptest.Server, methode, chemin, jetonCSRF string, form url.Values) *http.Response {
	t.Helper()
	req, err := http.NewRequest(methode, serveur.URL+chemin, strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-CSRF-Token", jetonCSRF)
	req.Header.Set("HX-Request", "true")
	rep, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return rep
}

func TestVlansParcoursCompletParHTTP(t *testing.T) {
	serveur, d := serveurAdressageDeTest(t)
	r := semerReseauDeTest(t, d)
	client := clientConnecte(t, serveur)
	jeton := jetonCSRFDepuisPage(t, client, serveur, "/vlans")

	rep, err := client.Get(serveur.URL + "/vlans")
	if err != nil {
		t.Fatal(err)
	}
	page := corps(t, rep)
	if rep.StatusCode != http.StatusOK || !strings.Contains(page, "Aucun VLAN") || !strings.Contains(page, `href="/vlans?export=csv"`) {
		t.Fatalf("page vide attendue avec ses boutons d'export : %d %s", rep.StatusCode, page)
	}
	if !strings.Contains(page, `<option value="`+id64(r.ClusterKafka)+`">Kafka (LOGS / PROD)</option>`) {
		t.Fatalf("les clusters doivent être proposés avec projet et environnement : %s", page)
	}

	// création : le tableau entier revient avec la ligne
	rep = requeteHTMX(t, client, serveur, http.MethodPost, "/vlans", jeton, url.Values{
		"code": {"VL-PROD-DC1"}, "projet_id": {id64(r.Projet)}, "environnement_id": {id64(r.Prod)},
		"zone_id": {id64(r.DC1)}, "commentaire": {"Production DC1"},
	})
	fragment := corps(t, rep)
	if rep.StatusCode != http.StatusOK || !strings.Contains(fragment, "VL-PROD-DC1") || !strings.Contains(fragment, "aucune plage") {
		t.Fatalf("création : %d %s", rep.StatusCode, fragment)
	}
	v, err := d.LireVlanParCode("VL-PROD-DC1")
	if err != nil || v.ProjetID == nil || v.ZoneID == nil || *v.ZoneID != r.DC1 || v.ClusterID != nil {
		t.Fatalf("VLAN créé mal renseigné : %+v, %v", v, err)
	}
	// code dupliqué : message métier, jamais 4xx
	rep = requeteHTMX(t, client, serveur, http.MethodPost, "/vlans", jeton, url.Values{"code": {"VL-PROD-DC1"}})
	if fragment = corps(t, rep); rep.StatusCode != http.StatusOK || !strings.Contains(fragment, "Ce code existe déjà") {
		t.Fatalf("doublon : %d %s", rep.StatusCode, fragment)
	}

	// plages : ajout, chevauchement refusé avec message, suppression
	chemin := "/vlans/" + id64(v.ID)
	rep = requeteHTMX(t, client, serveur, http.MethodPost, chemin+"/plages", jeton, url.Values{
		"ip_debut": {"10.10.1.10"}, "ip_fin": {"10.10.1.250"},
	})
	if fragment = corps(t, rep); !strings.Contains(fragment, "10.10.1.10–10.10.1.250") {
		t.Fatalf("ajout de plage : %s", fragment)
	}
	rep = requeteHTMX(t, client, serveur, http.MethodPost, chemin+"/plages", jeton, url.Values{
		"ip_debut": {"10.10.1.100"}, "ip_fin": {"10.10.1.120"},
	})
	if fragment = corps(t, rep); rep.StatusCode != http.StatusOK || !strings.Contains(fragment, "chevauchement") {
		t.Fatalf("chevauchement : %d %s", rep.StatusCode, fragment)
	}
	rep = requeteHTMX(t, client, serveur, http.MethodPost, chemin+"/plages", jeton, url.Values{
		"ip_debut": {"2001:db8::1"}, "ip_fin": {"2001:db8::9"},
	})
	if fragment = corps(t, rep); !strings.Contains(fragment, "IPv6") {
		t.Fatalf("IPv6 refusée avec un message clair : %s", fragment)
	}
	v, _ = d.LireVlan(v.ID)
	if len(v.Plages) != 1 {
		t.Fatalf("une seule plage attendue, obtenu %d", len(v.Plages))
	}

	// édition en ligne : le formulaire pré-sélectionne les critères
	rep, _ = client.Get(serveur.URL + chemin + "/editer")
	if fragment = corps(t, rep); !strings.Contains(fragment, `value="`+id64(r.DC1)+`" selected`) || !strings.Contains(fragment, `name="derniere_ip"`) {
		t.Fatalf("formulaire d'édition : %s", fragment)
	}
	rep = requeteHTMX(t, client, serveur, http.MethodPut, chemin, jeton, url.Values{
		"code": {"VL-PROD"}, "projet_id": {id64(r.Projet)}, "environnement_id": {id64(r.Prod)},
		"zone_id": {""}, "cluster_id": {id64(r.ClusterHot)}, "derniere_ip": {"10.10.1.42"},
		"commentaire": {"Production DC1"},
	})
	if fragment = corps(t, rep); !strings.Contains(fragment, "VL-PROD") || !strings.Contains(fragment, "ElasticHot (LOGS / PROD)") || !strings.Contains(fragment, "10.10.1.42") {
		t.Fatalf("modification : %s", fragment)
	}
	relu, _ := d.LireVlan(v.ID)
	if relu.ZoneID != nil || relu.ClusterID == nil || relu.DerniereIP == nil || *relu.DerniereIP != "10.10.1.42" {
		t.Fatalf("modification non appliquée : %+v", relu)
	}
	// curseur invalide : le formulaire est réaffiché avec l'erreur
	rep = requeteHTMX(t, client, serveur, http.MethodPut, chemin, jeton, url.Values{"code": {"VL-PROD"}, "derniere_ip": {"abc"}})
	if fragment = corps(t, rep); rep.StatusCode != http.StatusOK || !strings.Contains(fragment, `name="code"`) || !strings.Contains(fragment, "IPv4") {
		t.Fatalf("erreur de curseur : %d %s", rep.StatusCode, fragment)
	}

	// export : mêmes colonnes que l'écran, plages jointes
	rep, _ = client.Get(serveur.URL + "/vlans?export=csv")
	if csv := corps(t, rep); !strings.HasPrefix(csv, "Code;Projet;Environnement;Zone;Cluster;Plages;Curseur;Commentaire\n") ||
		!strings.Contains(csv, "VL-PROD;LOGS;PROD;;ElasticHot (LOGS / PROD);10.10.1.10–10.10.1.250;10.10.1.42;Production DC1") {
		t.Fatalf("export csv inattendu : %s", csv)
	}

	// suppression de plage puis du VLAN
	rep = requeteHTMX(t, client, serveur, http.MethodDelete, chemin+"/plages/"+id64(v.Plages[0].ID), jeton, nil)
	if fragment = corps(t, rep); strings.Contains(fragment, "10.10.1.10–10.10.1.250") {
		t.Fatalf("la plage devrait avoir disparu : %s", fragment)
	}
	rep = requeteHTMX(t, client, serveur, http.MethodDelete, chemin, jeton, nil)
	if fragment = corps(t, rep); !strings.Contains(fragment, "Aucun VLAN") {
		t.Fatalf("suppression du VLAN : %s", fragment)
	}
}

func TestVlansSuppressionRefuseeSiPorteParUnServeur(t *testing.T) {
	serveur, d := serveurAdressageDeTest(t)
	client := clientConnecte(t, serveur)
	jeton := jetonCSRFDepuisPage(t, client, serveur, "/vlans")

	v, err := d.CreerVlan(depot.Vlan{Code: "VL1"})
	if err != nil {
		t.Fatal(err)
	}
	nom := "srv-01"
	if _, err := d.CreerServeur(depot.Serveur{PhysicalName: &nom, Statut: depot.StatutEnService, VlanID: &v.ID}); err != nil {
		t.Fatal(err)
	}
	rep := requeteHTMX(t, client, serveur, http.MethodDelete, "/vlans/"+id64(v.ID), jeton, nil)
	fragment := corps(t, rep)
	if rep.StatusCode != http.StatusOK || !strings.Contains(fragment, "encore porté par au moins un serveur") || !strings.Contains(fragment, "VL1") {
		t.Fatalf("refus attendu avec message et VLAN toujours listé : %d %s", rep.StatusCode, fragment)
	}
}

// TestVlansLectureSeuleSansEcriture : un lecteur voit le catalogue mais ne
// peut pas le modifier (403 sur POST).
func TestVlansLectureSeulePourUnLecteur(t *testing.T) {
	serveur, d := serveurAdressageDeTest(t)
	hash, _ := auth.HacherMotDePasse("lecture!")
	if _, err := d.CreerUtilisateur(depot.Utilisateur{Login: "lecteur", Hash: hash, Role: depot.RoleLecteur}); err != nil {
		t.Fatal(err)
	}
	client := clientConnecteEnTantQue(t, serveur, "lecteur", "lecture!")
	jeton := jetonCSRFDepuisPage(t, client, serveur, "/vlans")
	rep := requeteHTMX(t, client, serveur, http.MethodPost, "/vlans", jeton, url.Values{"code": {"VL1"}})
	rep.Body.Close()
	if rep.StatusCode != http.StatusForbidden {
		t.Fatalf("lecteur : 403 attendu sur une écriture, obtenu %d", rep.StatusCode)
	}
}

// clientConnecteEnTantQue est clientConnecte pour un autre compte.
func clientConnecteEnTantQue(t *testing.T, serveur *httptest.Server, login, motDePasse string) *http.Client {
	t.Helper()
	client := clientConnecte(t, serveur) // pose le cookie jar et le suivi de redirection
	rep, err := client.PostForm(serveur.URL+"/connexion", url.Values{"login": {login}, "mot_de_passe": {motDePasse}})
	if err != nil {
		t.Fatal(err)
	}
	rep.Body.Close()
	if rep.StatusCode != http.StatusSeeOther {
		t.Fatalf("connexion %s : attendu 303, obtenu %d", login, rep.StatusCode)
	}
	return client
}

// id64 rend un identifiant en texte pour composer chemins et formulaires
// (itoa est déjà pris par handlers_demande_test.go, même paquet).
func id64(v int64) string { return strconv.FormatInt(v, 10) }
