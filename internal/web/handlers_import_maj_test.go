package web

import (
	"strings"
	"testing"

	"parallax/internal/depot"
)

// TestImportMajServeurs couvre l'import de mise à jour (v3.5) : clé par
// demande + fiche puis par hostname, colonnes absentes intactes, #VIDE,
// diff de simulation sans écriture, réaffectation datée, changement de
// modèle, refus d'un serveur inconnu, journal signé, export aller-retour.
func TestImportMajServeurs(t *testing.T) {
	serveur, d := serveurDeTest(t)
	client := clientConnecte(t, serveur)
	jetonCSRF := jetonCSRFDepuisPage(t, client, serveur, "/import/serveurs/maj")

	// fixtures : deux zones, deux clusters, deux modèles, deux serveurs.
	zone, cluster, revision := seedImportServeursFixtures(t, d)
	zone2, err := d.CreerZone(depot.Zone{Code: "DC2", Libelle: "Zone 2"})
	if err != nil {
		t.Fatal(err)
	}
	c1, err := d.LireCluster(cluster.ID)
	if err != nil {
		t.Fatal(err)
	}
	cluster2, err := d.CreerCluster(depot.Cluster{Nom: "ElasticCold", ProjetID: c1.ProjetID, EnvironnementID: c1.EnvironnementID, TechnoID: c1.TechnoID})
	if err != nil {
		t.Fatal(err)
	}
	m2, err := d.CreerModele(depot.Modele{Type: "DENSE", Annee: 2026, Code: "DENSE-2026"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.CreerRevision(depot.Revision{ModeleID: m2.ID, DateEffet: "2026-01-01"}); err != nil {
		t.Fatal(err)
	}
	nom, dem, fiche, comm := "PHY-100", "DEM-1", "SRV-1", "à effacer"
	s1, err := d.CreerServeur(depot.Serveur{PhysicalName: &nom, Statut: depot.StatutCommande, DemandeRef: &dem, DemandeServeurRef: &fiche, Commentaire: &comm, ZoneID: &zone.ID, DateEntree: ptrTexte("2026-01-01")})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.RattacherRevision(s1.ID, revision.ID, "2026-01-01", nil); err != nil {
		t.Fatal(err)
	}
	if err := d.Affecter(s1.ID, cluster.ID, "2026-02-01", nil, nil); err != nil {
		t.Fatal(err)
	}
	nom2 := "PHY-200"
	if _, err := d.CreerServeur(depot.Serveur{PhysicalName: &nom2, Statut: depot.StatutEnService, DateEntree: ptrTexte("2026-01-01")}); err != nil {
		t.Fatal(err)
	}

	// 1. réception : clé demande + fiche, pose hostname/série, efface le
	//    commentaire, change zone, passe EN_SERVICE ; PHY-200 sans changement.
	csv := "demande_ref;demande_serveur_ref;physical_name;hostname;serial_number;commentaire;zone_code;statut\n" +
		"DEM-1;SRV-1;;esh100;SN-100;#VIDE;DC2;EN_SERVICE\n" +
		";;PHY-200;;;;;EN_SERVICE\n"
	frag := posterCSVImport(t, client, serveur, jetonCSRF, "serveurs/maj", csv)
	for _, attendu := range []string{
		"1 serveur(s) à modifier, 1 sans changement",
		"hostname : — → esh100", "serial_number : — → SN-100", "commentaire : à effacer → —",
		"zone : " + zone.Code + " → DC2", "statut : COMMANDE → EN_SERVICE",
	} {
		if !strings.Contains(frag, attendu) {
			t.Fatalf("attendu « %s » dans la simulation : %s", attendu, frag)
		}
	}
	if avant, _ := d.LireServeur(s1.ID); avant.Hostname != nil {
		t.Fatal("l'analyse ne doit rien écrire")
	}
	fragConfirme := confirmerImport(t, client, serveur, jetonCSRF, "serveurs/maj", extraireJetonImport(t, frag))
	if !strings.Contains(fragConfirme, "1 serveur(s) mis à jour, 1 sans changement") {
		t.Fatalf("résumé final inattendu : %s", fragConfirme)
	}
	apres, _ := d.LireServeur(s1.ID)
	if apres.Hostname == nil || *apres.Hostname != "esh100" || apres.Commentaire != nil || apres.ZoneID == nil || *apres.ZoneID != zone2.ID ||
		apres.Statut != depot.StatutEnService || apres.PhysicalName == nil || *apres.PhysicalName != "PHY-100" {
		t.Fatalf("mise à jour attendue (hostname, commentaire effacé, zone, statut, nom physique intact) : %+v", apres)
	}
	journal, _ := d.ListerJournal(depot.FiltreJournal{Entite: "serveur", EntiteID: &s1.ID})
	if len(journal) < 2 || journal[0].UtilisateurLogin == nil || *journal[0].UtilisateurLogin != "gnocent" {
		t.Fatalf("les modifications doivent être journalisées au nom de gnocent : %+v", journal)
	}

	// 2. clé hostname : réaffectation datée et changement de modèle.
	csv2 := "hostname;projet_code;environnement_code;cluster_nom;date_affectation;modele_code;date_rattachement\n" +
		"esh100;LOGS;PROD;ElasticCold;2026-06-01;DENSE-2026;2026-06-01\n"
	frag2 := posterCSVImport(t, client, serveur, jetonCSRF, "serveurs/maj", csv2)
	if !strings.Contains(frag2, "affectation : "+cluster.Nom+" → ElasticCold à partir du 2026-06-01") || !strings.Contains(frag2, "→ DENSE-2026 rév. 1 à partir du 2026-06-01") {
		t.Fatalf("diff de réaffectation et de modèle attendu : %s", frag2)
	}
	confirmerImport(t, client, serveur, jetonCSRF, "serveurs/maj", extraireJetonImport(t, frag2))
	vies, _ := d.ListerAffectationsServeur(s1.ID)
	if len(vies) != 2 || vies[0].DateFin == nil || *vies[0].DateFin != "2026-05-31" || vies[1].ClusterID != cluster2.ID {
		t.Fatalf("réaffectation attendue : %+v", vies)
	}
	ratts, _ := d.ListerRattachements(s1.ID)
	if len(ratts) != 2 || ratts[1].DateDebut != "2026-06-01" {
		t.Fatalf("nouveau rattachement attendu : %+v", ratts)
	}

	// 3. refus : serveur inconnu, date manquante, doublon dans le fichier.
	csv3 := "hostname;physical_name;projet_code;environnement_code;cluster_nom\n" +
		"inconnu;;;;\n" +
		"esh100;;LOGS;PROD;" + cluster.Nom + "\n" +
		";PHY-100;;;\n"
	frag3 := posterCSVImport(t, client, serveur, jetonCSRF, "serveurs/maj", csv3)
	for _, attendu := range []string{"3 ligne(s) en erreur sur 3", "aucun serveur réel ne correspond", "date_affectation obligatoire", "déjà visé par la ligne 3"} {
		if !strings.Contains(frag3, attendu) {
			t.Fatalf("attendu « %s » : %s", attendu, frag3)
		}
	}

	// 4. export aller-retour : format exact, une ligne par serveur réel.
	rep, err := client.Get(serveur.URL + "/serveurs/export-maj")
	if err != nil {
		t.Fatal(err)
	}
	exp := corps(t, rep)
	if !strings.HasPrefix(exp, strings.Join(colonnesMaj, ";")+"\n") || !strings.Contains(exp, "PHY-100;esh100;SN-100;DC2;;;;;;;DEM-1;SRV-1;;EN_SERVICE;LOGS;PROD;ElasticCold;;DENSE-2026;") {
		t.Fatalf("export inattendu : %s", exp)
	}
}
