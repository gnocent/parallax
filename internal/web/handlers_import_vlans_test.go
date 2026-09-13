package web

import (
	"io/fs"
	"strings"
	"testing"

	"parallax/internal/depot"
)

// lireFichierExemple lit un fichier d'exemple depuis le FS embarqué
// (fichiersExemples, handlers_exemples.go) : exactement ce que sert la
// route de téléchargement /import/exemples/{nom}, donc un test qui l'importe
// couvre aussi la fraîcheur du fichier livré dans le binaire.
func lireFichierExemple(t *testing.T, nom string) string {
	t.Helper()
	contenu, err := fs.ReadFile(fichiersExemples, "exemples/"+nom)
	if err != nil {
		t.Fatalf("lecture de exemples/%s : %v", nom, err)
	}
	return string(contenu)
}

// Tests de l'import du catalogue réseau (backlog v3.1) : un VLAN par code,
// critères pris sur la première ligne, une ligne par plage ; analyse sans
// écriture, confirmation transactionnelle, refus de l'existant. Les helpers
// posterCSVImport / confirmerImport viennent de
// handlers_import_referentiels_test.go.

const csvVlansValide = "code;projet_code;environnement_code;zone_code;cluster_nom;ip_debut;ip_fin;commentaire\n" +
	"VL-LOGS-PROD-DC1;LOGS;PROD;DC1;;10.10.1.10;10.10.1.250;Production DC1\n" +
	"VL-LOGS-PROD-DC1;LOGS;PROD;DC1;;10.10.2.10;10.10.2.250;\n" +
	"VL-LOGS-PREPROD;LOGS;PREPROD;;;10.30.1.10;10.30.1.250;Pré-production\n" +
	"VL-KAFKA;LOGS;PROD;;Kafka;10.40.1.10;10.40.1.100;Réservé à Kafka\n" +
	"VL-ADMIN;;;;;;;Sans plage\n"

func TestImportVlansValideCreeVlansEtPlages(t *testing.T) {
	serveur, d := serveurAdressageDeTest(t)
	r := semerReseauDeTest(t, d)
	client := clientConnecte(t, serveur)
	jetonCSRF := jetonCSRFDepuisPage(t, client, serveur, "/import/vlans")

	fragment := posterCSVImport(t, client, serveur, jetonCSRF, "vlans", csvVlansValide)
	if !strings.Contains(fragment, "5 ligne(s) valide(s)") || !strings.Contains(fragment, "4 VLAN et 4 plage(s) prêt(s)") {
		t.Fatalf("résumé de simulation inattendu : %s", fragment)
	}
	if vlans, _ := d.ListerVlans(); len(vlans) != 0 {
		t.Fatalf("l'analyse ne doit rien écrire, trouvé %d VLAN", len(vlans))
	}

	fragmentConfirme := confirmerImport(t, client, serveur, jetonCSRF, "vlans", extraireJetonImport(t, fragment))
	if !strings.Contains(fragmentConfirme, "Import effectué : 4 VLAN et 4 plage(s)") {
		t.Fatalf("résumé final inattendu : %s", fragmentConfirme)
	}

	dc1, err := d.LireVlanParCode("VL-LOGS-PROD-DC1")
	if err != nil {
		t.Fatal(err)
	}
	if dc1.ProjetID == nil || *dc1.ProjetID != r.Projet || dc1.EnvironnementID == nil || *dc1.EnvironnementID != r.Prod ||
		dc1.ZoneID == nil || *dc1.ZoneID != r.DC1 || dc1.ClusterID != nil {
		t.Fatalf("critères de VL-LOGS-PROD-DC1 mal résolus : %+v", dc1)
	}
	if len(dc1.Plages) != 2 || dc1.Plages[0].IPDebut != "10.10.1.10" || dc1.Plages[1].IPFin != "10.10.2.250" {
		t.Fatalf("deux plages attendues sur VL-LOGS-PROD-DC1 : %+v", dc1.Plages)
	}
	if dc1.Commentaire == nil || *dc1.Commentaire != "Production DC1" {
		t.Fatalf("commentaire de la première ligne attendu : %v", dc1.Commentaire)
	}
	kafka, _ := d.LireVlanParCode("VL-KAFKA")
	if kafka.ClusterID == nil || *kafka.ClusterID != r.ClusterKafka || kafka.ZoneID != nil {
		t.Fatalf("VL-KAFKA doit viser le cluster Kafka seulement : %+v", kafka)
	}
	admin, _ := d.LireVlanParCode("VL-ADMIN")
	if admin.NbCriteres() != 0 || len(admin.Plages) != 0 {
		t.Fatalf("VL-ADMIN : aucun critère, aucune plage : %+v", admin)
	}

	// rejeu : tout existe déjà, refus intégral (une erreur par code, sur sa première ligne)
	fragmentRejoue := posterCSVImport(t, client, serveur, jetonCSRF, "vlans", csvVlansValide)
	if !strings.Contains(fragmentRejoue, "4 ligne(s) en erreur sur 5") || !strings.Contains(fragmentRejoue, "VLAN « VL-LOGS-PROD-DC1 » existe déjà") {
		t.Fatalf("le rejeu devrait refuser chaque code comme existant : %s", fragmentRejoue)
	}
}

func TestImportVlansErreursNEcriventRien(t *testing.T) {
	serveur, d := serveurAdressageDeTest(t)
	semerReseauDeTest(t, d)
	client := clientConnecte(t, serveur)
	jetonCSRF := jetonCSRFDepuisPage(t, client, serveur, "/import/vlans")

	csv := "code;projet_code;environnement_code;zone_code;cluster_nom;ip_debut;ip_fin\n" +
		"VL-OK;LOGS;PROD;DC1;;10.0.0.1;10.0.0.10\n" + // valide, mais rien ne sera écrit
		"VL-OK;LOGS;PROD;DC2;;10.0.1.1;10.0.1.10\n" + // critères différents de la ligne 2
		"VL-OK;LOGS;PROD;DC1;;10.0.0.5;10.0.0.20\n" + // chevauche la première plage
		"VL-INCONNU;STREAM;QUAL;DC9;;10.1.0.1;10.1.0.2\n" + // trois codes inconnus
		"VL-CLUSTER;;;;Kafka;10.2.0.1;10.2.0.2\n" + // cluster sans projet/environnement
		"VL-CLUSTER2;LOGS;PROD;;Nomad;10.2.0.1;10.2.0.2\n" + // cluster inconnu
		"VL-BORNE;;;;;10.3.0.1;\n" + // une seule borne
		"VL-ORDRE;;;;;10.3.0.9;10.3.0.1\n" + // début > fin
		"VL-V6;;;;;fe80::1;fe80::9\n" + // IPv6
		";;;;;10.4.0.1;10.4.0.2\n" // code vide
	fragment := posterCSVImport(t, client, serveur, jetonCSRF, "vlans", csv)
	for _, attendu := range []string{
		"9 ligne(s) en erreur sur 10",
		"Ligne 3 : VLAN « VL-OK » : critères différents de la ligne 2",
		"Ligne 4 : plage 10.0.0.5–10.0.0.20 : chevauchement",
		"Ligne 5 : projet « STREAM » inconnu ; environnement « QUAL » inconnu ; zone « DC9 » inconnu",
		"Ligne 6 : cluster_nom exige projet_code et environnement_code",
		"Ligne 7 : cluster « Nomad » inconnu pour projet_code « LOGS », environnement_code « PROD »",
		"Ligne 8 : ip_debut et ip_fin doivent être renseignés ensemble",
		"Ligne 9 : la plage 10.3.0.9–10.3.0.1 a un début après sa fin",
		"Ligne 10 : début de plage : donnée invalide : « fe80::1 » est une adresse IPv6",
		"Ligne 11 : code vide",
	} {
		if !strings.Contains(fragment, attendu) {
			t.Fatalf("attendu « %s » dans le rapport : %s", attendu, fragment)
		}
	}
	if vlans, _ := d.ListerVlans(); len(vlans) != 0 {
		t.Fatalf("un fichier en erreur ne doit rien écrire, obtenu %d VLAN", len(vlans))
	}

	// en-tête obligatoire absente
	fragment = posterCSVImport(t, client, serveur, jetonCSRF, "vlans", "projet_code;ip_debut;ip_fin\nLOGS;10.0.0.1;10.0.0.2\n")
	if !strings.Contains(fragment, "colonne(s) obligatoire(s) manquante(s) : code") {
		t.Fatalf("en-tête manquante attendue : %s", fragment)
	}
}

// TestImportVlansExempleDocs : le fichier livré dans docs/ passe l'analyse
// sur les référentiels et clusters de docs/*-exemple.csv (ici : le
// sous-ensemble semé par semerReseauDeTest, plus STREAM).
func TestImportVlansExempleDocsEstCoherent(t *testing.T) {
	serveur, d := serveurAdressageDeTest(t)
	semerReseauDeTest(t, d)
	if _, err := d.CreerProjet(depot.Projet{Code: "STREAM", Libelle: "Flux d'événements"}); err != nil {
		t.Fatal(err)
	}
	client := clientConnecte(t, serveur)
	jetonCSRF := jetonCSRFDepuisPage(t, client, serveur, "/import/vlans")

	contenu := lireFichierExemple(t, "vlans-exemple.csv")
	fragment := posterCSVImport(t, client, serveur, jetonCSRF, "vlans", contenu)
	if !strings.Contains(fragment, "7 ligne(s) valide(s)") || !strings.Contains(fragment, "6 VLAN et 6 plage(s) prêt(s)") {
		t.Fatalf("docs/vlans-exemple.csv devrait être valide : %s", fragment)
	}
}
