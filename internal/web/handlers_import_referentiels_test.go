package web

import (
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"parallax/internal/depot"
)

// Tests des imports de référentiels et de clusters : analyse sans écriture,
// confirmation dans une transaction, refus des codes inconnus, des doublons
// et de l'existant. Le serveur complet (serveurDeTest) suffit : les routes
// sont câblées dans server.go.

// posterCSVImport envoie un CSV en multipart vers /import/<chemin>/analyser.
func posterCSVImport(t *testing.T, client *http.Client, serveur *httptest.Server, jetonCSRF, chemin, contenu string) string {
	t.Helper()
	var corpsMultipart strings.Builder
	ecrivain := multipart.NewWriter(&corpsMultipart)
	part, err := ecrivain.CreateFormFile("fichier", chemin+".csv")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte(contenu)); err != nil {
		t.Fatal(err)
	}
	if err := ecrivain.Close(); err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, serveur.URL+"/import/"+chemin+"/analyser", strings.NewReader(corpsMultipart.String()))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", ecrivain.FormDataContentType())
	req.Header.Set("X-CSRF-Token", jetonCSRF)
	rep, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if rep.StatusCode != http.StatusOK {
		t.Fatalf("analyse %s : attendu 200, obtenu %d", chemin, rep.StatusCode)
	}
	return corps(t, rep)
}

// confirmerImport poste le jeton vers /import/<chemin>/confirmer.
func confirmerImport(t *testing.T, client *http.Client, serveur *httptest.Server, jetonCSRF, chemin, jetonImport string) string {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, serveur.URL+"/import/"+chemin+"/confirmer",
		strings.NewReader(url.Values{"jeton": {jetonImport}}.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-CSRF-Token", jetonCSRF)
	rep, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if rep.StatusCode != http.StatusOK {
		t.Fatalf("confirmation %s : attendu 200, obtenu %d", chemin, rep.StatusCode)
	}
	return corps(t, rep)
}

const csvReferentielsValide = "type;code;libelle;ordre;site\n" +
	"PROJET;LOGS;Plateforme de logs;;\n" +
	"environnement;PROD;Production;1;\n" + // type en minuscules : accepté
	"ENVIRONNEMENT;PREPROD;Pré-production;2;\n" +
	"TECHNO;ELASTIC;Elasticsearch;;\n" +
	"TECHNO;KAFKA;Kafka;;\n" +
	"TIER;HOT;Données chaudes;1;\n" +
	"USAGE;INGEST;Ingestion;;\n" +
	"ZONE;AZ1;Zone 1;;Site A\n" +
	"ZONE;AZ2;Zone 2;;\n"

func TestImportReferentielsValideCreeTout(t *testing.T) {
	serveur, d := serveurDeTest(t)
	client := clientConnecte(t, serveur)
	jetonCSRF := jetonCSRFDepuisPage(t, client, serveur, "/import/referentiels")

	fragment := posterCSVImport(t, client, serveur, jetonCSRF, "referentiels", csvReferentielsValide)
	if !strings.Contains(fragment, "9 ligne(s) valide(s)") || !strings.Contains(fragment, "2 environnement(s)") {
		t.Fatalf("résumé de simulation inattendu : %s", fragment)
	}
	if zones, _ := d.ListerZones(); len(zones) != 0 {
		t.Fatalf("l'analyse ne doit rien écrire, trouvé %d zone(s)", len(zones))
	}

	fragmentConfirme := confirmerImport(t, client, serveur, jetonCSRF, "referentiels", extraireJetonImport(t, fragment))
	if !strings.Contains(fragmentConfirme, "Import effectué") || !strings.Contains(fragmentConfirme, "2 zone(s)") {
		t.Fatalf("résumé final inattendu : %s", fragmentConfirme)
	}

	env, err := d.LireEnvironnementParCode("PREPROD")
	if err != nil || env.Ordre != 2 || env.Libelle != "Pré-production" {
		t.Fatalf("environnement PREPROD attendu avec ordre 2 : %+v, %v", env, err)
	}
	zone, err := d.LireZoneParCode("AZ1")
	if err != nil || zone.Site == nil || *zone.Site != "Site A" {
		t.Fatalf("zone AZ1 attendue avec site « Site A » : %+v, %v", zone, err)
	}
	if zone2, _ := d.LireZoneParCode("AZ2"); zone2.Site != nil {
		t.Fatalf("zone AZ2 sans site attendue, obtenu %v", *zone2.Site)
	}
	if p, err := d.LireProjetParCode("LOGS"); err != nil || !p.Actif {
		t.Fatalf("projet LOGS attendu actif : %+v, %v", p, err)
	}

	// un second import du même fichier est refusé intégralement : tout existe déjà.
	fragmentRejoue := posterCSVImport(t, client, serveur, jetonCSRF, "referentiels", csvReferentielsValide)
	if !strings.Contains(fragmentRejoue, "9 ligne(s) en erreur") || !strings.Contains(fragmentRejoue, "projet « LOGS » existe déjà") {
		t.Fatalf("le rejeu devrait refuser chaque ligne comme existante : %s", fragmentRejoue)
	}
}

func TestImportReferentielsErreursNEcriventRien(t *testing.T) {
	serveur, d := serveurDeTest(t)
	client := clientConnecte(t, serveur)
	jetonCSRF := jetonCSRFDepuisPage(t, client, serveur, "/import/referentiels")

	csv := "type;code;libelle;ordre\n" +
		"PROJET;LOGS;Plateforme de logs;\n" +
		"PLANETE;X;Type inconnu;\n" +
		"TIER;HOT;Chaud;1\n" +
		"TIER;HOT;Chaud encore;2\n" +
		"ENVIRONNEMENT;PROD;Production;abc\n" +
		"ZONE;;Sans code;\n"
	fragment := posterCSVImport(t, client, serveur, jetonCSRF, "referentiels", csv)
	for _, attendu := range []string{
		"5 ligne(s) en erreur sur 6",
		"Ligne 3 : type « PLANETE » inconnu",
		"Ligne 4 : tier « HOT » dupliqué dans le fichier (lignes 4 et 5)",
		"Ligne 6 : ordre :",
		"Ligne 7 : code vide",
	} {
		if !strings.Contains(fragment, attendu) {
			t.Fatalf("attendu « %s » dans le rapport : %s", attendu, fragment)
		}
	}
	if _, err := d.LireProjetParCode("LOGS"); err == nil {
		t.Fatal("la ligne valide ne doit pas être écrite quand une autre est en erreur")
	}
}

func TestImportClustersResoutLesCodes(t *testing.T) {
	serveur, d := serveurDeTest(t)
	client := clientConnecte(t, serveur)
	jetonCSRF := jetonCSRFDepuisPage(t, client, serveur, "/import/referentiels")

	frag := posterCSVImport(t, client, serveur, jetonCSRF, "referentiels", csvReferentielsValide)
	confirmerImport(t, client, serveur, jetonCSRF, "referentiels", extraireJetonImport(t, frag))

	csv := "nom;projet_code;environnement_code;techno_code;tier_code;usage_code;commentaire\n" +
		"ElasticHot;LOGS;PROD;ELASTIC;HOT;;Indexation temps réel\n" +
		"Kafka;LOGS;PROD;KAFKA;;INGEST;\n" +
		"ElasticHot;LOGS;PREPROD;ELASTIC;HOT;;\n" // même nom, autre environnement : permis
	fragment := posterCSVImport(t, client, serveur, jetonCSRF, "clusters", csv)
	if !strings.Contains(fragment, "3 cluster(s) prêt(s)") {
		t.Fatalf("résumé de simulation inattendu : %s", fragment)
	}
	if clusters, _ := d.ListerClusters(depot.FiltreCluster{}); len(clusters) != 0 {
		t.Fatalf("l'analyse ne doit rien écrire, trouvé %d cluster(s)", len(clusters))
	}
	fragmentConfirme := confirmerImport(t, client, serveur, jetonCSRF, "clusters", extraireJetonImport(t, fragment))
	if !strings.Contains(fragmentConfirme, "3 cluster(s) importé(s)") {
		t.Fatalf("résumé final inattendu : %s", fragmentConfirme)
	}

	clusters, err := d.ListerClusters(depot.FiltreCluster{})
	if err != nil || len(clusters) != 3 {
		t.Fatalf("attendu 3 clusters, obtenu %d (%v)", len(clusters), err)
	}
	techKafka, _ := d.LireTechnoParCode("KAFKA")
	usageIngest, _ := d.LireUsageParCode("INGEST")
	var kafka *depot.Cluster
	for i := range clusters {
		if clusters[i].Nom == "Kafka" {
			kafka = &clusters[i]
		}
	}
	if kafka == nil || kafka.TechnoID != techKafka.ID || kafka.TierID != nil ||
		kafka.UsageFonctionnelID == nil || *kafka.UsageFonctionnelID != usageIngest.ID {
		t.Fatalf("cluster Kafka mal résolu : %+v", kafka)
	}

	// codes inconnus, doublon dans le fichier et cluster existant : refus, rien d'écrit.
	csvErreurs := "nom;projet_code;environnement_code;techno_code;tier_code\n" +
		"ElasticHot;LOGS;PROD;ELASTIC;HOT\n" +
		"Nomad;LOGS;PROD;NOMAD;\n" +
		"Kafka2;STREAM;PROD;KAFKA;GLACE\n" +
		"Dup;LOGS;PROD;KAFKA;\n" +
		"Dup;LOGS;PROD;ELASTIC;\n"
	fragmentErreurs := posterCSVImport(t, client, serveur, jetonCSRF, "clusters", csvErreurs)
	for _, attendu := range []string{
		"5 ligne(s) en erreur sur 5",
		"Ligne 2 : cluster « ElasticHot » existe déjà",
		"Ligne 3 : techno « NOMAD » inconnu",
		"Ligne 4 : projet « STREAM » inconnu ; tier « GLACE » inconnu",
		"Ligne 5 : cluster « Dup » dupliqué dans le fichier (lignes 5 et 6)",
	} {
		if !strings.Contains(fragmentErreurs, attendu) {
			t.Fatalf("attendu « %s » dans le rapport : %s", attendu, fragmentErreurs)
		}
	}
	if clusters, _ := d.ListerClusters(depot.FiltreCluster{}); len(clusters) != 3 {
		t.Fatalf("un fichier en erreur ne doit rien écrire, obtenu %d clusters", len(clusters))
	}
}
